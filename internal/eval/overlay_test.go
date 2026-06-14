package eval_test

import (
	"path/filepath"
	"testing"

	"cuelang.org/go/cue/load"

	"github.com/lugoues/creidhne"
	"github.com/lugoues/creidhne/internal/eval"
)

// TestOverlayOffline proves an end-user project resolves the schema purely from
// an overlay built off the *embedded* SchemaFS (no on-disk dependency, symlink,
// or network), exactly as the shipped binary does for a user who only
// `import "github.com/lugoues/creidhne@v0"`.
func TestOverlayOffline(t *testing.T) {
	tmp := t.TempDir()

	overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	// Supply the user's project via the overlay too, so tmp stays empty on disk.
	overlay[filepath.Join(tmp, "cue.mod", "module.cue")] = load.FromString(
		"module: \"example.com/demo@v0\"\nlanguage: version: \"v0.16.0\"\n")
	overlay[filepath.Join(tmp, "main.cue")] = load.FromString(`package demo

import q "github.com/lugoues/creidhne@v0"

app: q.#Quadlet & {
	name: "demo"
	units: #container: {
		Container: {Image: "docker.io/nginx:latest", ContainerName: "demo"}
		Install: WantedBy: ["default.target"]
	}
}
`)

	quads, err := eval.LoadAndValidate(tmp, overlay)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(quads) != 1 {
		t.Fatalf("got %d quadlets, want 1", len(quads))
	}
	q := quads[0]
	if q.Name != "demo" || len(q.Units) != 1 {
		t.Fatalf("unexpected quadlet: %+v", q)
	}
	u := q.Units[0]
	if u.Kind != "container" || u.Filename != "demo.container" {
		t.Fatalf("unexpected unit: kind=%q filename=%q", u.Kind, u.Filename)
	}
	container, _ := u.Data["Container"].(map[string]any)
	if container["Image"] != "docker.io/nginx:latest" {
		t.Fatalf("unexpected image: %v", container["Image"])
	}
}

// TestDiscoverNestedQuadlets proves quadlets are found at the top level AND
// nested inside a grouping struct (e.g. `stacks: web:`), not silently dropped.
func TestDiscoverNestedQuadlets(t *testing.T) {
	tmp := t.TempDir()
	overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(tmp, "cue.mod", "module.cue")] = load.FromString(
		"module: \"example.com/demo@v0\"\nlanguage: version: \"v0.16.0\"\n")
	overlay[filepath.Join(tmp, "main.cue")] = load.FromString(`package demo

import q "github.com/lugoues/creidhne@v0"

app: q.#Quadlet & {
	name: "app"
	units: #container: Container: {Image: "img-app", ContainerName: "app"}
}
stacks: web: q.#Quadlet & {
	name: "web"
	units: #container: Container: {Image: "img-web", ContainerName: "web"}
}
`)

	quads, err := eval.LoadAndValidate(tmp, overlay)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	names := map[string]bool{}
	for _, q := range quads {
		names[q.Name] = true
	}
	if !names["app"] || !names["web"] {
		t.Fatalf("expected both top-level (app) and nested (web) quadlets, got %v", names)
	}
}
