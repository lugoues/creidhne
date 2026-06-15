package reconcile

import (
	"os"
	"os/exec"
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

func TestListExistingSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "app.container"), "x")
	write(t, filepath.Join(dir, "images", "real.Containerfile"), "FROM x")

	// Symlinks named like managed files must NOT be scheduled for removal: crei
	// does not own them, and removing one (RemoveAll) would delete the link.
	if err := os.Symlink(filepath.Join(dir, "app.container"), filepath.Join(dir, "link.container")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "images", "real.Containerfile"), filepath.Join(dir, "images", "link.Containerfile")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	got, err := ListExisting(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"app.container", "images/real.Containerfile"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ListExisting must skip symlinks:\n got:  %v\n want: %v", got, want)
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

// TestComputePlanModeDrift: a build-context file whose content is unchanged but
// whose explicit mode drifted on disk must be re-applied, not reported as
// unchanged.
func TestComputePlanModeDrift(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "images", "script.sh")
	write(t, p, "#!/bin/sh\n")
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	desired := map[string]DesiredFile{
		"images/script.sh": {Content: []byte("#!/bin/sh\n"), Mode: "0755"},
	}
	changes, err := ComputePlan(desired, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Action != ActionChange || changes[0].Mode != "0755" {
		t.Fatalf("want one ActionChange(mode 0755), got %+v", changes)
	}

	// Same content AND matching mode -> Unchanged.
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	changes, err = ComputePlan(desired, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Action != ActionUnchanged {
		t.Fatalf("want ActionUnchanged when mode matches, got %+v", changes)
	}
}

// TestComputePlanFileDirTransitions: a path whose on-disk type conflicts with
// the desired type must be classified (not hard-error).
func TestComputePlanFileDirTransitions(t *testing.T) {
	// dir on disk, desired wants a regular file there -> Change
	d1 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d1, "images", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(d1, "images", "foo", "bar"), "x")
	changes, err := ComputePlan(map[string]DesiredFile{"images/foo": {Content: []byte("now-a-file")}}, d1)
	if err != nil {
		t.Fatalf("dir->file: unexpected error %v", err)
	}
	if got := actionFor(changes, "images/foo"); got != ActionChange {
		t.Fatalf("dir->file: want Change for images/foo, got %v", got)
	}

	// file on disk where desired nests under it -> Add (ancestor cleared on write)
	d2 := t.TempDir()
	write(t, filepath.Join(d2, "images", "foo"), "old")
	changes, err = ComputePlan(map[string]DesiredFile{"images/foo/bar": {Content: []byte("nested")}}, d2)
	if err != nil {
		t.Fatalf("file->dir: unexpected error %v", err)
	}
	if got := actionFor(changes, "images/foo/bar"); got != ActionAdd {
		t.Fatalf("file->dir: want Add for images/foo/bar, got %v", got)
	}
}

// TestWriteFileReplacesDirectory: WriteFile clears a directory occupying the
// target path and writes the regular file.
func TestWriteFileReplacesDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "images", "ctx")
	write(t, filepath.Join(target, "leftover"), "junk")
	if err := WriteFile(target, []byte("file-content"), ""); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(b) != "file-content" {
		t.Fatalf("want file-content, got %q", b)
	}
}

func actionFor(changes []Change, name string) ActionKind {
	for _, c := range changes {
		if c.Name == name {
			return c.Action
		}
	}
	return ActionKind(-1)
}

// TestRunDiffMissingToolErrors: a diff tool that can't be run surfaces an error
// instead of returning an empty (false "no changes") diff.
func TestRunDiffMissingToolErrors(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "f")
	write(t, live, "a\n")
	if _, err := RunDiff(live, []byte("b\n"), "live", "new", "crei-no-such-difftool-zzz"); err == nil {
		t.Fatal("expected error for a missing diff tool")
	}
}

// TestRunDiffExternalToolNonZeroExit: a tool that exits non-zero because files
// differ (here cmp) is not treated as an error; its output is returned.
func TestRunDiffExternalToolNonZeroExit(t *testing.T) {
	if _, err := exec.LookPath("cmp"); err != nil {
		t.Skip("cmp not available")
	}
	dir := t.TempDir()
	live := filepath.Join(dir, "f")
	write(t, live, "a\n")
	out, err := RunDiff(live, []byte("b\n"), "live", "new", "cmp")
	if err != nil {
		t.Fatalf("non-zero exit (files differ) should not error: %v", err)
	}
	if out == "" {
		t.Fatal("expected output from the diff tool")
	}
}

// TestWriteFileAtomicLeavesNoTemp: the atomic write renames its temp into place,
// leaving only the target file (no .crei-*.tmp residue).
func TestWriteFileAtomicLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFile(filepath.Join(dir, "a.container"), []byte("x"), "0644"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "a.container" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected only a.container, got %v", names)
	}
}
