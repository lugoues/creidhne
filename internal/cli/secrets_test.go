package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/lugoues/creidhne/internal/podman"
)

// A project whose registry declares app-db (explicit name) and api_key (name
// defaults to the key).
const secretsRegistryMain = `package config

import q "github.com/lugoues/creidhne@v0"

secrets: q.#SecretRegistry & {
	app_db:  {name: "app-db"}
	api_key: _
}
`

// stubSecrets swaps the podman/valuer hooks for the duration of a test (podman
// isn't available, and the huh form needs a TTY). Managed defaults to empty;
// remove and read fail the test if called unexpectedly.
func stubSecrets(t *testing.T, existing map[string]bool, create func(string, []byte, bool) error, value func(string) ([]byte, bool, error)) {
	t.Helper()
	ol, om, oc, orm, ord, ov := podmanListSecrets, podmanSecretInfos, podmanCreateSecret, podmanRemoveSecret, podmanReadSecret, secretValuer
	t.Cleanup(func() {
		podmanListSecrets, podmanSecretInfos, podmanCreateSecret, podmanRemoveSecret, podmanReadSecret, secretValuer = ol, om, oc, orm, ord, ov
	})
	podmanListSecrets = func() (map[string]bool, error) { return existing, nil }
	podmanSecretInfos = func() (map[string]podman.SecretInfo, error) {
		infos := map[string]podman.SecretInfo{}
		for n := range existing {
			infos[n] = podman.SecretInfo{}
		}
		return infos, nil
	}
	podmanCreateSecret = create
	podmanRemoveSecret = func(name string) error { t.Errorf("unexpected RemoveSecret(%q)", name); return nil }
	podmanReadSecret = func(name string) ([]byte, error) { t.Errorf("unexpected ReadSecret(%q)", name); return nil, nil }
	secretValuer = value
}

// TestCmdSecretsList drives `crei secrets list` end to end (registry extraction +
// the podman existence check, with podman stubbed). app-db exists, api_key does
// not, exercising both the name-override and default-name registry forms.
func TestCmdSecretsList(t *testing.T) {
	dir := setupProject(t, secretsRegistryMain)
	stubSecrets(t, map[string]bool{"app-db": true}, nil, nil)
	podmanSecretInfos = func() (map[string]podman.SecretInfo, error) {
		created := time.Now().Add(-2 * time.Hour)
		return map[string]podman.SecretInfo{"app-db": {Managed: true, CreatedAt: created, UpdatedAt: created}}, nil
	}

	out, err := runCmd(t, "--dir", dir, "secrets", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"app-db", "present", "api_key", "missing", "1 present, 1 missing", "CREATED", "2h ago"} {
		if !strings.Contains(out, want) {
			t.Errorf("secrets output missing %q:\n%s", want, out)
		}
	}
}

// TestCmdSecretsNoRegistry: with no registry, the command reports that and never
// shells out to podman.
func TestCmdSecretsNoRegistry(t *testing.T) {
	dir := setupProject(t, testMain) // a quadlet, no `secrets` registry
	called := false
	stubSecrets(t, nil, nil, nil)
	podmanListSecrets = func() (map[string]bool, error) { called = true; return nil, nil }

	out, err := runCmd(t, "--dir", dir, "secrets", "list")
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

// TestCmdSecretsHelp: bare `crei secrets` prints help listing its subcommands
// (no RunE), mirroring `podman secret`, and never touches podman.
func TestCmdSecretsHelp(t *testing.T) {
	called := false
	stubSecrets(t, nil, nil, nil)
	podmanListSecrets = func() (map[string]bool, error) { called = true; return nil, nil }

	out, err := runCmd(t, "--dir", t.TempDir(), "secrets")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"list", "create"} {
		if !strings.Contains(out, want) {
			t.Errorf("`crei secrets` help should list the %q subcommand:\n%s", want, out)
		}
	}
	if called {
		t.Error("bare `crei secrets` should not query podman")
	}

	// An unknown subcommand errors (matches podman), rather than printing help
	// with a success exit code.
	if _, err := runCmd(t, "--dir", t.TempDir(), "secrets", "bogus"); err == nil {
		t.Error("`crei secrets bogus` should error on an unknown subcommand")
	}
}

// TestCmdSecretsCreateAll: -a creates every registry secret missing from podman.
func TestCmdSecretsCreateAll(t *testing.T) {
	dir := setupProject(t, secretsRegistryMain)
	created := map[string]string{}
	stubSecrets(t, map[string]bool{}, // none exist yet
		func(name string, v []byte, _ bool) error { created[name] = string(v); return nil },
		func(name string) ([]byte, bool, error) { return []byte("v-" + name), false, nil },
	)

	out, err := runCmd(t, "--dir", dir, "secrets", "create", "-a")
	if err != nil {
		t.Fatal(err)
	}
	if created["app-db"] != "v-app-db" || created["api_key"] != "v-api_key" {
		t.Fatalf("expected both secrets created, got %v", created)
	}
	if !strings.Contains(out, "created") {
		t.Errorf("want a 'created' line:\n%s", out)
	}
}

// TestCmdSecretsCreateSkipsExisting: creating an existing secret without
// --replace skips it and never calls create.
func TestCmdSecretsCreateSkipsExisting(t *testing.T) {
	dir := setupProject(t, secretsRegistryMain)
	created := map[string]string{}
	stubSecrets(t, map[string]bool{"app-db": true},
		func(name string, v []byte, _ bool) error { created[name] = string(v); return nil },
		func(string) ([]byte, bool, error) { return []byte("x"), false, nil },
	)

	out, err := runCmd(t, "--dir", dir, "secrets", "create", "app-db")
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 0 {
		t.Fatalf("existing secret should be skipped, created %v", created)
	}
	if !strings.Contains(out, "already exists") {
		t.Errorf("want a skip notice:\n%s", out)
	}
}

// TestCmdSecretsCreateArgs: exactly one of a name or -a is required.
func TestCmdSecretsCreateArgs(t *testing.T) {
	dir := setupProject(t, secretsRegistryMain)
	stubSecrets(t, map[string]bool{},
		func(string, []byte, bool) error { return nil },
		func(string) ([]byte, bool, error) { return nil, false, nil },
	)
	if _, err := runCmd(t, "--dir", dir, "secrets", "create"); err == nil {
		t.Error("create with neither a name nor -a should error")
	}
	if _, err := runCmd(t, "--dir", dir, "secrets", "create", "-a", "app-db"); err == nil {
		t.Error("create with both a name and -a should error")
	}
}

// A project with the registry plus units referencing secrets outside it: a
// raw container Secret= string and a build Secret= entry.
const secretsPruneMain = secretsRegistryMain + `
app: q.#Quadlet & {
	name: "app"
	units: {
		#container: Container: {
			Image: "docker.io/x"
			Secret: ["raw-ref,type=env,target=TOKEN"]
		}
		#build: {ContainerFile: "FROM alpine\n", Build: Secret: ["build-ref"]}
	}
}
`

// TestCmdSecretsPrune: managed ∧ unreferenced is deleted; referenced managed
// secrets survive (registry, raw container ref, build ref); unlabeled ones
// are skipped as not-created-by-crei.
func TestCmdSecretsPrune(t *testing.T) {
	dir := setupProject(t, secretsPruneMain)
	stubSecrets(t, map[string]bool{
		"app-db":    true, // registry, managed
		"raw-ref":   true, // container ref, managed
		"build-ref": true, // build ref, managed
		"old-one":   true, // managed, unreferenced -> delete
		"stray":     true, // unmanaged, unreferenced -> skip
	}, nil, nil)
	podmanSecretInfos = func() (map[string]podman.SecretInfo, error) {
		return map[string]podman.SecretInfo{
			"app-db":    {Managed: true},
			"raw-ref":   {Managed: true},
			"build-ref": {Managed: true},
			"old-one":   {Managed: true},
			"stray":     {},
		}, nil
	}
	var removed []string
	podmanRemoveSecret = func(name string) error { removed = append(removed, name); return nil }

	out, err := runCmd(t, "--dir", dir, "secrets", "prune", "-y")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(removed) != 1 || removed[0] != "old-one" {
		t.Fatalf("expected exactly old-one removed, got %v", removed)
	}
	for _, want := range []string{"delete old-one", "1 to delete", "kept (referenced)", "skipped (not created by crei)"} {
		if !strings.Contains(out, want) {
			t.Errorf("prune output missing %q:\n%s", want, out)
		}
	}
}

// TestCmdSecretsPruneNothing: all managed secrets referenced -> no deletions,
// no prompt, exit zero.
func TestCmdSecretsPruneNothing(t *testing.T) {
	dir := setupProject(t, secretsRegistryMain)
	stubSecrets(t, map[string]bool{"app-db": true, "stray": true}, nil, nil)
	podmanSecretInfos = func() (map[string]podman.SecretInfo, error) {
		return map[string]podman.SecretInfo{"app-db": {Managed: true}, "stray": {}}, nil
	}

	out, err := runCmd(t, "--dir", dir, "secrets", "prune", "-y")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Nothing to prune") {
		t.Errorf("want 'Nothing to prune':\n%s", out)
	}
}

// TestCmdSecretsAdopt: existing unlabeled registry secrets are re-created
// with their value byte-exact; managed and missing ones are left alone.
func TestCmdSecretsAdopt(t *testing.T) {
	dir := setupProject(t, secretsRegistryMain)
	created := map[string]string{}
	replaced := map[string]bool{}
	stubSecrets(t, map[string]bool{"app-db": true, "api_key": true},
		func(name string, v []byte, replace bool) error {
			created[name] = string(v)
			replaced[name] = replace
			return nil
		}, nil)
	podmanSecretInfos = func() (map[string]podman.SecretInfo, error) {
		return map[string]podman.SecretInfo{"app-db": {}, "api_key": {Managed: true}}, nil
	}
	podmanReadSecret = func(name string) ([]byte, error) { return []byte("value-of-" + name + "\n"), nil }

	out, err := runCmd(t, "--dir", dir, "secrets", "adopt")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(created) != 1 || created["app-db"] != "value-of-app-db\n" || !replaced["app-db"] {
		t.Fatalf("expected app-db re-created byte-exact with replace, got %v (replace=%v)", created, replaced)
	}
	if !strings.Contains(out, "app-db adopted") {
		t.Errorf("want adoption notice:\n%s", out)
	}
}

func TestGeneratePassword(t *testing.T) {
	a, err := generatePassword(32)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 {
		t.Fatalf("length %d, want 32", len(a))
	}
	for _, c := range a {
		if !strings.ContainsRune(passwordCharset, rune(c)) {
			t.Fatalf("generated byte %q not in charset", c)
		}
	}
	if b, _ := generatePassword(32); string(a) == string(b) {
		t.Error("two generations should differ")
	}
	if d, _ := generatePassword(0); len(d) != 32 {
		t.Errorf("n<=0 should default to 32, got %d", len(d))
	}
}
