package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lugoues/creidhne/internal/eval"
)

func sampleQuads() []eval.Quadlet {
	return []eval.Quadlet{{
		Name: "app",
		Units: []eval.UnitRecord{{
			Kind: "container", Stem: "app", Filename: "app.container", Service: "app.service",
			Data: map[string]any{"Container": map[string]any{"Image": "docker.io/x"}},
		}},
	}}
}

func sampleFiles(content string) map[string]map[string]FileInput {
	return map[string]map[string]FileInput{
		"app": {"app.container": {Content: []byte(content), Mode: "0644"}},
	}
}

func TestLoadAbsentReturnsNil(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil || s != nil {
		t.Fatalf("Load(absent) = %v, %v; want nil, nil", s, err)
	}
}

func TestWriteLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	s := Build("v1.9.0", now, nil, sampleQuads(), sampleFiles("[Container]\n"))
	if err := Write(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	q, ok := got.Quadlets["app"]
	if !ok || len(q.Files) != 1 || len(q.Units) != 1 {
		t.Fatalf("round-trip lost data: %+v", got)
	}
	f := q.Files[0]
	if f.Path != "app.container" || f.SHA256 != HashBytes([]byte("[Container]\n")) || f.Mode != "0644" || !f.AppliedAt.Equal(now) {
		t.Fatalf("file record mismatch: %+v", f)
	}
	if u := q.Units[0]; u.Service != "app.service" || u.Kind != "container" {
		t.Fatalf("unit record mismatch: %+v", u)
	}
	// No stray temp files from the atomic write.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected only %s in dir, got %d entries", Filename, len(entries))
	}
}

func TestLoadCorruptErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("corrupt state should error, not be silently ignored")
	}
}

// TestBuildCarriesForwardAppliedAt: an unchanged file keeps its original
// AppliedAt across a later apply; a changed file gets the new time.
func TestBuildCarriesForwardAppliedAt(t *testing.T) {
	t1 := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	first := Build("v1", t1, nil, sampleQuads(), sampleFiles("same"))

	unchanged := Build("v1", t2, first, sampleQuads(), sampleFiles("same"))
	if got := unchanged.Quadlets["app"].Files[0].AppliedAt; !got.Equal(t1) {
		t.Fatalf("unchanged file AppliedAt = %v, want carried-forward %v", got, t1)
	}
	if got := unchanged.Quadlets["app"].AppliedAt; !got.Equal(t2) {
		t.Fatalf("quadlet AppliedAt should be the latest apply, got %v", got)
	}

	changed := Build("v1", t2, first, sampleQuads(), sampleFiles("different"))
	if got := changed.Quadlets["app"].Files[0].AppliedAt; !got.Equal(t2) {
		t.Fatalf("changed file AppliedAt = %v, want %v", got, t2)
	}
}

func TestEqualIgnoresTimestamps(t *testing.T) {
	t1 := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	a := Build("v1", t1, nil, sampleQuads(), sampleFiles("same"))
	b := Build("v2", t1.Add(time.Hour), nil, sampleQuads(), sampleFiles("same"))
	if !Equal(a, b) {
		t.Fatal("states with identical content should be Equal despite timestamps/version")
	}
	c := Build("v1", t1, nil, sampleQuads(), sampleFiles("different"))
	if Equal(a, c) {
		t.Fatal("states with different content hashes must not be Equal")
	}
	if Equal(a, nil) || !Equal(nil, nil) {
		t.Fatal("nil handling wrong")
	}
}

func TestFileOwnerAndRecord(t *testing.T) {
	s := Build("v1", time.Now(), nil, sampleQuads(), sampleFiles("x"))
	if owner := s.FileOwner()["app.container"]; owner != "app" {
		t.Fatalf("owner = %q, want app", owner)
	}
	if _, ok := s.FileRecord("app.container"); !ok {
		t.Fatal("FileRecord should find app.container")
	}
	if _, ok := s.FileRecord("nope"); ok {
		t.Fatal("FileRecord should miss unknown paths")
	}
	var nilState *State
	if nilState.FileOwner() != nil {
		t.Fatal("nil state FileOwner should be nil")
	}
	if _, ok := nilState.FileRecord("x"); ok {
		t.Fatal("nil state FileRecord should miss")
	}
}
