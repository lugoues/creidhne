package render

import (
	"os"
	"strings"
	"testing"

	"github.com/lugoues/creidhne/internal/eval"
)

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
