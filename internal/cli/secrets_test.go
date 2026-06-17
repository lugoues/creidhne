package cli

import (
	"strings"
	"testing"
)

// TestCmdSecretsList drives `crei secrets` end to end (registry extraction +
// the podman existence check, with podman stubbed). app-db exists, api_key does
// not, exercising both the name-override and default-name registry forms.
func TestCmdSecretsList(t *testing.T) {
	dir := setupProject(t, `package config

import q "github.com/lugoues/creidhne@v0"

secrets: q.#SecretRegistry & {
	app_db:  {name: "app-db"}
	api_key: _
}
`)
	orig := podmanListSecrets
	defer func() { podmanListSecrets = orig }()
	podmanListSecrets = func() (map[string]bool, error) {
		return map[string]bool{"app-db": true}, nil // api_key missing
	}

	out, err := runCmd(t, "--dir", dir, "secrets")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"app-db", "present", "api_key", "missing", "1 present, 1 missing"} {
		if !strings.Contains(out, want) {
			t.Errorf("secrets output missing %q:\n%s", want, out)
		}
	}
}

// TestCmdSecretsNoRegistry: with no registry, the command reports that and never
// shells out to podman.
func TestCmdSecretsNoRegistry(t *testing.T) {
	dir := setupProject(t, testMain) // a quadlet, no `secrets` registry
	orig := podmanListSecrets
	defer func() { podmanListSecrets = orig }()
	called := false
	podmanListSecrets = func() (map[string]bool, error) { called = true; return nil, nil }

	out, err := runCmd(t, "--dir", dir, "secrets")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No secrets declared") {
		t.Errorf("want 'No secrets declared', got:\n%s", out)
	}
	if called {
		t.Error("podman must not be queried when there is no registry")
	}
}
