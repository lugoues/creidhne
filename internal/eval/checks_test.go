package eval_test

import (
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue/load"

	"github.com/lugoues/creidhne"
	"github.com/lugoues/creidhne/internal/eval"
)

// A helper-style mixin registering a check, defined in the consumer package
// (the cross-package proof: #checks unifies from outside creidhne).
const checkedMixin = `package naming
import creidhne "github.com/lugoues/creidhne@v0"

#Spec: {
	#cfg: {
		port!: int
		mode?: "a" | "b"
	}
	#checks: "spec/cfg": {
		require: [#cfg.port]
		why:     "fill #cfg when mixing #Spec"
	}
	// Mixins stay open at the top; #Quadlet enforces the field set.
	...
}
`

// TestChecksRequireUnsetFails: an unfilled mixin config must fail the load
// with the check's name and why, not render inert.
func TestChecksRequireUnsetFails(t *testing.T) {
	err := loadSourceErr(t, checkedMixin+`
app: creidhne.#Quadlet & #Spec & {
	name: "app"
	units: #container: Container: Image: "docker.io/x"
}
`)
	if err == nil {
		t.Fatal("unfilled required check must fail the load")
	}
	for _, want := range []string{`check "spec/cfg" failed`, "fill #cfg when mixing #Spec", "quadlet app"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("check failure missing %q:\n%v", want, err)
		}
	}
}

// TestChecksPassIsInvisible: a satisfied check changes nothing about the
// decoded quadlet.
func TestChecksPassIsInvisible(t *testing.T) {
	quads := loadSource(t, checkedMixin+`
app: creidhne.#Quadlet & #Spec & {
	name: "app"
	#cfg: port: 8080
	units: #container: Container: Image: "docker.io/x"
}
`)
	if len(quads) != 1 || len(quads[0].Units) != 1 {
		t.Fatalf("unexpected decode: %+v", quads)
	}
	if img := quads[0].Units[0].Data["Container"].(map[string]any)["Image"]; img != "docker.io/x" {
		t.Fatalf("unit data disturbed: %v", img)
	}
}

// TestChecksFailStrictValidate: the crei validate path (eval.Validate) maps
// check failures to name/why too.
func TestChecksFailStrictValidate(t *testing.T) {
	tmp := t.TempDir()
	overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(tmp, "cue.mod", "module.cue")] = load.FromString(
		"module: \"example.com/naming@v0\"\nlanguage: version: \"v0.17.0\"\n")
	overlay[filepath.Join(tmp, "main.cue")] = load.FromString(checkedMixin + `
app: creidhne.#Quadlet & #Spec & {
	name: "app"
	units: #container: Container: Image: "docker.io/x"
}
`)
	err = eval.Validate(tmp, overlay)
	if err == nil {
		t.Fatal("validate must fail on an unfilled required check")
	}
	for _, want := range []string{`check "spec/cfg" failed`, "fill #cfg when mixing #Spec"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validate failure missing %q:\n%v", want, err)
		}
	}
}

// TestChecksAssertFails: a false assertion is a load error carrying the why.
func TestChecksAssertFails(t *testing.T) {
	err := loadSourceErr(t, `package naming
import creidhne "github.com/lugoues/creidhne@v0"

app: creidhne.#Quadlet & {
	name: "app"
	#checks: "app/ports": {
		assert: 9000 < 100
		why:    "port must stay below 100"
	}
	units: #container: Container: Image: "docker.io/x"
}
`)
	if err == nil {
		t.Fatal("false assertion must fail the load")
	}
	if !strings.Contains(err.Error(), "port must stay below 100") && !strings.Contains(err.Error(), "app/ports") {
		t.Fatalf("assertion failure lost its context:\n%v", err)
	}
}
