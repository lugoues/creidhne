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
