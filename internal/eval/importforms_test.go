package eval_test

import (
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue/load"

	"github.com/lugoues/creidhne"
	"github.com/lugoues/creidhne/internal/eval"
)

// multiFileProject builds an overlay project from named files.
func multiFileProject(t *testing.T, files map[string]string) (string, map[string]load.Source) {
	t.Helper()
	tmp := t.TempDir()
	overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(tmp, "cue.mod", "module.cue")] = load.FromString(
		"module: \"example.com/naming@v0\"\nlanguage: version: \"v0.16.0\"\n")
	for name, src := range files {
		overlay[filepath.Join(tmp, name)] = load.FromString(src)
	}
	return tmp, overlay
}

// TestMixedImportFormsRejected reproduces the production failure: a #self
// built in an @v0 file consumed by an unversioned-import file loads the
// schema as two packages and the _kind discriminators stop unifying. The
// guard must catch it at load with an actionable message instead.
func TestMixedImportFormsRejected(t *testing.T) {
	dir, overlay := multiFileProject(t, map[string]string{
		"net.cue": `package naming
import q "github.com/lugoues/creidhne@v0"

egress: q.#Quadlet & {name: "egress", units: #network: {}}
`,
		"app.cue": `package naming
import q "github.com/lugoues/creidhne"

app: q.#Quadlet & {
	name: "app"
	units: #container: Container: {
		Image:   "docker.io/x"
		Network: [egress.units.#network.#self]
	}
}
`,
	})
	_, err := eval.LoadAndValidate(dir, overlay)
	if err == nil {
		t.Fatal("mixed import forms must be rejected")
	}
	for _, want := range []string{"conflicting creidhne import forms", `"github.com/lugoues/creidhne"`, `"github.com/lugoues/creidhne@v0"`, "app.cue", "net.cue"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("guard message missing %q:\n%v", want, err)
		}
	}
}

// TestConsistentImportFormsAccepted: the same project with one form works.
func TestConsistentImportFormsAccepted(t *testing.T) {
	dir, overlay := multiFileProject(t, map[string]string{
		"net.cue": `package naming
import q "github.com/lugoues/creidhne@v0"

egress: q.#Quadlet & {name: "egress", units: #network: {}}
`,
		"app.cue": `package naming
import q "github.com/lugoues/creidhne@v0"

app: q.#Quadlet & {
	name: "app"
	units: #container: Container: {
		Image:   "docker.io/x"
		Network: [egress.units.#network.#self]
	}
}
`,
	})
	quads, err := eval.LoadAndValidate(dir, overlay)
	if err != nil {
		t.Fatalf("consistent forms must load: %v", err)
	}
	if len(quads) != 2 {
		t.Fatalf("expected 2 quadlets, got %d", len(quads))
	}
}
