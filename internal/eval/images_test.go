package eval_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lugoues/creidhne"
	"github.com/lugoues/creidhne/internal/eval"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadImageRegistry decodes the registries package, covering the three
// digest states — pinned, explicitly empty, and omitted. The empty-string
// case is the regression: an empty digest must load (not fail the schema).
func TestLoadImageRegistry(t *testing.T) {
	tmp := t.TempDir()
	overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tmp, "cue.mod", "module.cue"),
		"module: \"example.com/demo@v0\"\nlanguage: version: \"v0.17.0\"\n")
	writeFile(t, filepath.Join(tmp, "registries", "images.cue"), `package registries
import "github.com/lugoues/creidhne"
images: creidhne.#ImageRegistry & {
	pinned:  {image: "docker.io/x/y:v1", digest: "sha256:abc", minAge: "7d"}
	empty:   {image: "docker.io/x/y:v1", digest: ""}
	omitted: image: "docker.io/x/y:v1"
}
`)

	entries, err := eval.LoadImageRegistry(tmp, overlay)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := map[string]eval.ImageEntry{}
	for _, e := range entries {
		got[e.Key] = e
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(got), entries)
	}
	if p := got["pinned"]; p.Image != "docker.io/x/y:v1" || p.Digest != "sha256:abc" || p.MinAge != "7d" {
		t.Fatalf("pinned decoded wrong: %+v", p)
	}
	if got["empty"].Digest != "" || got["omitted"].Digest != "" {
		t.Fatalf("empty/omitted digest must be \"\": %+v %+v", got["empty"], got["omitted"])
	}
}

// TestLoadImageRegistryAbsent: no registries package returns (nil, nil).
func TestLoadImageRegistryAbsent(t *testing.T) {
	tmp := t.TempDir()
	overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := eval.LoadImageRegistry(tmp, overlay)
	if err != nil || entries != nil {
		t.Fatalf("absent registry = %v, %v; want nil, nil", entries, err)
	}
}
