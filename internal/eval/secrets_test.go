package eval_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"cuelang.org/go/cue/load"

	"github.com/lugoues/creidhne"
	"github.com/lugoues/creidhne/internal/eval"
)

func secretsProject(t *testing.T, mainCue string) (string, map[string]load.Source) {
	t.Helper()
	tmp := t.TempDir()
	overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(tmp, "cue.mod", "module.cue")] = load.FromString(
		"module: \"example.com/demo@v0\"\nlanguage: version: \"v0.16.0\"\n")
	overlay[filepath.Join(tmp, "main.cue")] = load.FromString(mainCue)
	return tmp, overlay
}

// TestSecretRegistryNames: each entry's name defaults to its key, an explicit
// name overrides it, and the result is deduplicated and sorted.
func TestSecretRegistryNames(t *testing.T) {
	tmp, overlay := secretsProject(t, `package demo

import q "github.com/lugoues/creidhne@v0"

secrets: q.#SecretRegistry & {
	db_password: _
	tls:         {name: "tls-cert"}
	dup:         {name: "tls-cert"}
}
`)
	names, err := eval.SecretRegistry(tmp, overlay, "secrets")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"db_password", "tls-cert"} // duplicate collapsed, sorted
	if !reflect.DeepEqual(names, want) {
		t.Errorf("SecretRegistry() = %v, want %v", names, want)
	}
}

// TestSecretRegistryMissingField: no registry field yields no names, no error
// (so `crei secrets` works in a project that declares none).
func TestSecretRegistryMissingField(t *testing.T) {
	tmp, overlay := secretsProject(t, `package demo

import q "github.com/lugoues/creidhne@v0"

app: q.#Quadlet & {
	name: "app"
	units: #container: Container: {Image: "img", ContainerName: "app"}
}
`)
	names, err := eval.SecretRegistry(tmp, overlay, "secrets")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Errorf("want no names, got %v", names)
	}
}

// TestSecretRegistryCustomField: the registry can live under a configurable name.
func TestSecretRegistryCustomField(t *testing.T) {
	tmp, overlay := secretsProject(t, `package demo

import q "github.com/lugoues/creidhne@v0"

vault: q.#SecretRegistry & {api_key: _}
`)
	names, err := eval.SecretRegistry(tmp, overlay, "vault")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"api_key"}; !reflect.DeepEqual(names, want) {
		t.Errorf("SecretRegistry(vault) = %v, want %v", names, want)
	}
}
