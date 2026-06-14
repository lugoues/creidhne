package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/kinds"
)

// TestEveryKindHasTemplate guards the shared kinds table against drift: every
// kind the renderer/reconciler manage must have a template to render it.
func TestEveryKindHasTemplate(t *testing.T) {
	for kind := range kinds.Ext {
		if _, err := os.Stat(filepath.Join("../../templates", kind+".tpl")); err != nil {
			t.Errorf("kind %q in kinds.Ext has no template: %v", kind, err)
		}
	}
}

// newTestRenderer loads the on-disk templates so the render tests don't depend
// on the embedded FS.
func newTestRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := New(os.DirFS("../../templates"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func containerUnit(quadlet, stem string) eval.UnitRecord {
	return eval.UnitRecord{
		Kind:     "container",
		Stem:     stem,
		Filename: stem + ".container",
		Data: map[string]any{
			"Container": map[string]any{"Image": quadlet, "ContainerName": stem},
		},
	}
}

// TestBuildFileSetDuplicateFilename ensures two units resolving to the same
// filename produce a hard error instead of a silent overwrite.
func TestBuildFileSetDuplicateFilename(t *testing.T) {
	r := newTestRenderer(t)
	quads := []eval.Quadlet{
		{Name: "app-web", Units: []eval.UnitRecord{containerUnit("img-A", "app-web")}},
		{Name: "app", Units: []eval.UnitRecord{containerUnit("img-B", "app-web")}},
	}
	_, err := r.BuildFileSet(quads)
	if err == nil {
		t.Fatal("expected duplicate-filename error, got nil")
	}
	if !strings.Contains(err.Error(), "app-web.container") {
		t.Errorf("error should name the colliding file, got: %v", err)
	}
}

// TestRenderZeroAndFalseValues ensures schema-valid falsy values (integer 0,
// boolean false) are rendered rather than silently dropped by `{{ if }}`.
func TestRenderZeroAndFalseValues(t *testing.T) {
	r := newTestRenderer(t)
	cu := eval.UnitRecord{
		Kind: "container", Stem: "z", Filename: "z.container",
		Data: map[string]any{"Container": map[string]any{
			"Image":             "img",
			"ContainerName":     "z",
			"StopTimeout":       int64(0),
			"HealthMaxLogCount": int64(0),
			"Notify":            false,
		}},
	}
	vu := eval.UnitRecord{
		Kind: "volume", Stem: "v", Filename: "v.volume",
		Data: map[string]any{"Volume": map[string]any{"UID": int64(0), "GID": int64(0)}},
	}
	files, err := r.BuildFileSet([]eval.Quadlet{{Name: "z", Units: []eval.UnitRecord{cu, vu}}})
	if err != nil {
		t.Fatalf("BuildFileSet: %v", err)
	}
	for _, want := range []string{"StopTimeout=0", "HealthMaxLogCount=0", "Notify=false"} {
		if !strings.Contains(string(files["z.container"].Content), want) {
			t.Errorf("z.container missing %q:\n%s", want, files["z.container"].Content)
		}
	}
	for _, want := range []string{"UID=0", "GID=0"} {
		if !strings.Contains(string(files["v.volume"].Content), want) {
			t.Errorf("v.volume missing %q:\n%s", want, files["v.volume"].Content)
		}
	}
}

// TestBuildContextModes covers the build-context mode normalization: a plain
// string entry defaults to 0644, a {content, mode} entry keeps its explicit
// mode. This is the only coverage of the mode pipeline (the golden harness
// compares content only).
func TestBuildContextModes(t *testing.T) {
	r := newTestRenderer(t)
	bu := eval.UnitRecord{
		Kind: "build", Stem: "x", Filename: "x.build",
		Data: map[string]any{
			"ContainerFile": "FROM scratch\n",
			"Context": map[string]any{
				"plain.txt": "hello",
				"run.sh":    map[string]any{"content": "#!/bin/sh\n", "mode": "0755"},
			},
			"Build": map[string]any{"ImageTag": []any{"localhost/x:latest"}},
		},
	}
	files, err := r.BuildFileSet([]eval.Quadlet{{Name: "x", Units: []eval.UnitRecord{bu}}})
	if err != nil {
		t.Fatalf("BuildFileSet: %v", err)
	}
	if got := files["images/x.context/plain.txt"].Mode; got != "0644" {
		t.Errorf("plain string entry mode = %q, want 0644", got)
	}
	if got := files["images/x.context/run.sh"].Mode; got != "0755" {
		t.Errorf("explicit-mode entry mode = %q, want 0755", got)
	}
}

// TestBuildFileSetDistinctFilenames is the happy path: no collision.
func TestBuildFileSetDistinctFilenames(t *testing.T) {
	r := newTestRenderer(t)
	quads := []eval.Quadlet{
		{Name: "a", Units: []eval.UnitRecord{containerUnit("img-A", "a")}},
		{Name: "b", Units: []eval.UnitRecord{containerUnit("img-B", "b")}},
	}
	files, err := r.BuildFileSet(quads)
	if err != nil {
		t.Fatalf("BuildFileSet: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d: %v", len(files), files)
	}
}
