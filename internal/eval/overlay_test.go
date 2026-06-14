package eval_test

import (
	"path/filepath"
	"strings"
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

// TestValidateRejectsIncomplete proves Validate fails on a non-concrete value
// that never reaches a rendered unit, while LoadAndValidate (render/apply path)
// still succeeds because the value isn't part of any unit's data.
func TestValidateRejectsIncomplete(t *testing.T) {
	tmp := t.TempDir()
	overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(tmp, "cue.mod", "module.cue")] = load.FromString(
		"module: \"example.com/demo@v0\"\nlanguage: version: \"v0.16.0\"\n")
	overlay[filepath.Join(tmp, "main.cue")] = load.FromString(`package demo

import q "github.com/lugoues/creidhne@v0"

mustBeSet: string // incomplete, never rendered

app: q.#Quadlet & {
	name: "app"
	units: #container: Container: {Image: "img", ContainerName: "app"}
}
`)

	if err := eval.Validate(tmp, overlay); err == nil {
		t.Fatal("Validate: expected incomplete-value error, got nil")
	}
	if _, err := eval.LoadAndValidate(tmp, overlay); err != nil {
		t.Fatalf("LoadAndValidate should ignore non-rendered incompleteness, got %v", err)
	}
}

// TestIncompleteUnitConciseError ensures an incomplete unit (here: a Container
// missing both Image and Rootfs) produces a short, named error rather than a
// multi-KB dump of the whole resolved struct.
func TestIncompleteUnitConciseError(t *testing.T) {
	tmp := t.TempDir()
	overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(tmp, "cue.mod", "module.cue")] = load.FromString(
		"module: \"example.com/demo@v0\"\nlanguage: version: \"v0.16.0\"\n")
	overlay[filepath.Join(tmp, "main.cue")] = load.FromString(`package demo

import q "github.com/lugoues/creidhne@v0"

a: q.#Quadlet & {
	name: "z"
	units: #container: Container: {ContainerName: "x"} // no Image/Rootfs
}
`)

	_, err = eval.LoadAndValidate(tmp, overlay)
	if err == nil {
		t.Fatal("expected an incomplete-unit error, got nil")
	}
	if len(err.Error()) > 400 {
		t.Fatalf("error should be concise, got %d bytes:\n%s", len(err.Error()), err.Error())
	}
	if !strings.Contains(err.Error(), "z.container") {
		t.Errorf("error should name the unit, got: %v", err)
	}
}
