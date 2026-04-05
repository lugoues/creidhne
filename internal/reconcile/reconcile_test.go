package reconcile

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListExisting(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "app.container"), "x")
	write(t, filepath.Join(dir, "data.volume"), "x")
	write(t, filepath.Join(dir, "notes.txt"), "ignored")                   // wrong extension
	if err := os.Mkdir(filepath.Join(dir, "app.pod"), 0o755); err != nil { // dir named like a unit
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "images", "app.Containerfile"), "FROM x")
	write(t, filepath.Join(dir, "images", "app.context", "etc", "conf"), "y")

	got, err := ListExisting(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{
		"app.container",
		"data.volume",
		"images/app.Containerfile",
		"images/app.context/etc/conf",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ListExisting:\n got:  %v\n want: %v", got, want)
	}
	// Safety invariant: no directory (e.g. the app.pod dir or images/ subdirs)
	// is ever returned.
	for _, g := range got {
		if g == "app.pod" || strings.HasSuffix(g, "/etc") {
			t.Fatalf("ListExisting returned a directory: %q", g)
		}
	}
}

func TestListExistingMissingDir(t *testing.T) {
	got, err := ListExisting(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want empty, nil", got, err)
	}
}

func TestComputePlanAndSummary(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "keep.container"), "same")
	write(t, filepath.Join(dir, "edit.container"), "old")
	write(t, filepath.Join(dir, "stale.container"), "gone")
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	desired := map[string]DesiredFile{
		"keep.container": {Content: []byte("same")},
		"edit.container": {Content: []byte("new")},
		"add.container":  {Content: []byte("fresh"), Mode: "0644"},
	}

	changes, err := ComputePlan(desired, dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ActionKind{}
	for _, c := range changes {
		got[c.Name] = c.Action
	}
	wantAction := map[string]ActionKind{
		"add.container":   ActionAdd,
		"edit.container":  ActionChange,
		"keep.container":  ActionUnchanged,
		"stale.container": ActionRemove,
	}
	for name, want := range wantAction {
		if got[name] != want {
			t.Errorf("%s: action %v, want %v", name, got[name], want)
		}
	}
	// A directory must never be planned for removal.
	if _, ok := got["subdir"]; ok {
		t.Errorf("directory subdir was planned as a change")
	}
	// Change carries the existing content for diffing.
	for _, c := range changes {
		if c.Name == "edit.container" && string(c.Existing) != "old" {
			t.Errorf("edit existing = %q, want old", c.Existing)
		}
	}

	s := Summarize(changes)
	if s.Added != 1 || s.Changed != 1 || s.Unchanged != 1 || s.Removed != 1 {
		t.Errorf("summary = %+v", s)
	}
	if s.Total != 3 { // total excludes removed
		t.Errorf("total = %d, want 3 (excludes removed)", s.Total)
	}
}

func TestWriteFileCreatesParentsAndMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "images", "app.context", "scripts", "entrypoint.sh")
	if err := WriteFile(path, []byte("#!/bin/sh\n"), "0755"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %o, want 0755", fi.Mode().Perm())
	}
}

func TestWriteFileToDirectoryTransition(t *testing.T) {
	dir := t.TempDir()
	// A stale flat file sits where a directory is now needed.
	write(t, filepath.Join(dir, "images"), "stale")
	path := filepath.Join(dir, "images", "app.Containerfile")
	if err := WriteFile(path, []byte("FROM x"), ""); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "FROM x" {
		t.Fatalf("read back: %q, %v", b, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "images")); err != nil || !fi.IsDir() {
		t.Fatalf("images should be a directory now: %v", err)
	}
}

func TestRemoveFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.container")
	write(t, f, "x")
	if err := RemoveFile(f); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("file not removed")
	}
	tree := filepath.Join(dir, "images", "app.context")
	write(t, filepath.Join(tree, "nested", "file"), "y")
	if err := RemoveFile(tree); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Errorf("tree not removed")
	}
}

func TestPruneEmptyDirs(t *testing.T) {
	dir := t.TempDir()
	images := filepath.Join(dir, "images")
	if err := os.MkdirAll(filepath.Join(images, "empty", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(images, "full", "keep"), "x")

	if err := PruneEmptyDirs(images); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(images, "empty")); !os.IsNotExist(err) {
		t.Errorf("empty tree should have been pruned")
	}
	if _, err := os.Stat(filepath.Join(images, "full")); err != nil {
		t.Errorf("non-empty dir should be preserved: %v", err)
	}
	// Root is never removed.
	if _, err := os.Stat(images); err != nil {
		t.Errorf("root images/ should be preserved: %v", err)
	}
	// Nonexistent is a no-op.
	if err := PruneEmptyDirs(filepath.Join(dir, "nope")); err != nil {
		t.Errorf("prune nonexistent: %v", err)
	}
}

func TestRunDiffInternal(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "a.container")
	write(t, live, "line1\nline2\n")
	out, err := RunDiff(live, []byte("line1\nCHANGED\n"), "live/a", "new/a", "diff")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "-line2") || !strings.Contains(out, "+CHANGED") {
		t.Errorf("unexpected diff:\n%s", out)
	}
}
