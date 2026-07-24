package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lugoues/creidhne/internal/eval"
)

// TestEmitSecretRegistry: entries survive emit -> load unchanged, the file is
// tab-indented, and the name field is emitted only when it differs from the
// key. The registry is rewritten wholesale, so a dropped field is silent data
// loss from the user's config.
func TestEmitSecretRegistry(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "cue.mod", "module.cue"),
		"module: \"example.com/demo@v0\"\nlanguage: version: \"v0.17.0\"\n")

	entries := []eval.SecretEntry{
		{Key: "db_password", Name: "db_password", Generate: &eval.SecretGenerate{Length: 40}},
		{Key: "tls_cert", Name: "prod-tls"}, // manual (no generate), name override
		{Key: "api_key", Name: "api_key", Generate: &eval.SecretGenerate{Length: 24, Charset: "hex"}},
	}
	content, err := emitSecretRegistry(entries)
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)
	for _, want := range []string{
		"package registries",
		"secrets: creidhne.#SecretRegistry & {",
		"db_password: generate: {length: 40}",
		`tls_cert: name: "prod-tls"`,
		`api_key: generate: {length: 24, charset: "hex"}`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("emit missing %q:\n%s", want, s)
		}
	}
	// db_password's name equals its key, so no name field is emitted for it.
	if strings.Contains(s, `db_password: {name:`) {
		t.Fatalf("name should be omitted when it equals the key:\n%s", s)
	}
	for i, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, " ") {
			t.Fatalf("line %d is space-indented, want tabs: %q", i+1, line)
		}
	}

	mustWrite(t, filepath.Join(dir, "registries", "secrets.cue"), s)
	got, err := eval.LoadSecretRegistry(dir, overlayFor(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]eval.SecretEntry{}
	for _, e := range got {
		by[e.Key] = e
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(got), got)
	}
	if g := by["db_password"].Generate; g == nil || g.Length != 40 {
		t.Fatalf("db_password generate round-tripped wrong: %+v", by["db_password"])
	}
	if by["tls_cert"].Name != "prod-tls" || by["tls_cert"].Generate != nil {
		t.Fatalf("tls_cert round-tripped wrong: %+v", by["tls_cert"])
	}
	if g := by["api_key"].Generate; g == nil || g.Length != 24 || g.Charset != "hex" {
		t.Fatalf("api_key generate round-tripped wrong: %+v", by["api_key"])
	}
}

// TestCmdSecretAdd registers a generated and a manual secret, then confirms
// both land in the crei-owned registry and are visible to `secret list`.
func TestCmdSecretAdd(t *testing.T) {
	dir := setupProject(t, testMain) // a quadlet, no hand-authored secrets field
	stubSecrets(t, nil, nil, nil)

	if _, err := runCmd(t, "--dir", dir, "secret", "add", "db_password", "--length", "40"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCmd(t, "--dir", dir, "secret", "add", "tls_cert", "--manual", "--name", "prod-tls"); err != nil {
		t.Fatal(err)
	}

	entries, err := eval.LoadSecretRegistry(dir, overlayFor(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 registered secrets, got %+v", entries)
	}

	// list reconciles the crei-owned registry against podman: prod-tls is the
	// podman name of tls_cert, db_password its own key.
	out, err := runCmd(t, "--dir", dir, "secret", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"db_password", "prod-tls", "missing"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}
}

// TestCmdSecretRemove: remove unregisters from the crei-owned registry, and
// with --delete also removes the value from podman (confirmed via -y).
func TestCmdSecretRemove(t *testing.T) {
	dir := setupProject(t, testMain)
	stubSecrets(t, map[string]bool{"db_password": true}, nil, nil)

	if _, err := runCmd(t, "--dir", dir, "secret", "add", "db_password"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCmd(t, "--dir", dir, "secret", "add", "keep_me"); err != nil {
		t.Fatal(err)
	}

	// An unknown name errors before writing anything (checked while the
	// registry is non-empty; an empty registry reports "nothing to remove").
	if _, err := runCmd(t, "--dir", dir, "secret", "remove", "nope"); err == nil {
		t.Fatal("removing an unknown secret should error")
	}

	// Plain remove: unregisters, never touches podman (the stub's RemoveSecret
	// fails the test if called).
	if _, err := runCmd(t, "--dir", dir, "secret", "remove", "keep_me"); err != nil {
		t.Fatal(err)
	}
	entries, _ := eval.LoadSecretRegistry(dir, overlayFor(t, dir))
	if len(entries) != 1 || entries[0].Key != "db_password" {
		t.Fatalf("after remove, want only db_password, got %+v", entries)
	}

	// --delete -y removes from podman too.
	removed := ""
	podmanRemoveSecret = func(name string) error { removed = name; return nil }
	if _, err := runCmd(t, "--dir", dir, "secret", "remove", "db_password", "--delete", "-y"); err != nil {
		t.Fatal(err)
	}
	if removed != "db_password" {
		t.Fatalf("--delete should remove from podman, got %q", removed)
	}
	entries, _ = eval.LoadSecretRegistry(dir, overlayFor(t, dir))
	if len(entries) != 0 {
		t.Fatalf("registry should be empty after removing both, got %+v", entries)
	}
}

// TestGenerateFromPolicy: each charset yields a value of the requested length
// drawn from the right alphabet.
func TestGenerateFromPolicy(t *testing.T) {
	cases := []struct {
		charset string
		length  int
		allowed func(rune) bool
	}{
		{"", 32, func(r rune) bool { return strings.ContainsRune(passwordCharset, r) }},
		{"alphanumeric", 17, func(r rune) bool { return strings.ContainsRune(passwordCharset, r) }},
		{"hex", 40, func(r rune) bool { return strings.ContainsRune("0123456789abcdef", r) }},
		{"base64", 43, func(r rune) bool {
			return strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", r)
		}},
	}
	for _, c := range cases {
		v, err := generateFromPolicy(&eval.SecretGenerate{Length: c.length, Charset: c.charset})
		if err != nil {
			t.Fatalf("%s: %v", c.charset, err)
		}
		if len(v) != c.length {
			t.Fatalf("%s: len %d, want %d", c.charset, len(v), c.length)
		}
		for _, r := range string(v) {
			if !c.allowed(r) {
				t.Fatalf("%s: value %q has out-of-alphabet rune %q", c.charset, v, r)
			}
		}
	}
	// Two draws differ (crypto/rand, not a fixed seed).
	a, _ := generateFromPolicy(&eval.SecretGenerate{Length: 32})
	b, _ := generateFromPolicy(&eval.SecretGenerate{Length: 32})
	if string(a) == string(b) {
		t.Fatal("two generated values should differ")
	}
}

// TestCmdSecretRotate: rotate regenerates generate-policy entries into podman
// (replace=true) and skips manual ones.
func TestCmdSecretRotate(t *testing.T) {
	dir := setupProject(t, testMain)
	created := map[string][]byte{}
	replaced := map[string]bool{}
	stubSecrets(t, nil, func(name string, value []byte, replace bool) error {
		created[name] = value
		replaced[name] = replace
		return nil
	}, nil)

	if _, err := runCmd(t, "--dir", dir, "secret", "add", "db_password", "--length", "40"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCmd(t, "--dir", dir, "secret", "add", "tls_cert", "--manual"); err != nil {
		t.Fatal(err)
	}

	// Rotate everything (-y): db_password rotates, tls_cert (manual) is skipped.
	out, err := runCmd(t, "--dir", dir, "secret", "rotate", "-y")
	if err != nil {
		t.Fatal(err)
	}
	if len(created["db_password"]) != 40 || !replaced["db_password"] {
		t.Fatalf("db_password should be replaced with a 40-char value, got %d chars replace=%v", len(created["db_password"]), replaced["db_password"])
	}
	if _, ok := created["tls_cert"]; ok {
		t.Fatal("a manual secret must not be auto-rotated")
	}
	if !strings.Contains(out, "tls_cert") || !strings.Contains(out, "manual") {
		t.Errorf("rotate should note the skipped manual entry:\n%s", out)
	}

	// Rotating a manual entry by name surfaces it as skipped, not an error.
	out, err = runCmd(t, "--dir", dir, "secret", "rotate", "tls_cert", "-y")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Nothing to rotate") {
		t.Errorf("rotating only a manual entry should rotate nothing:\n%s", out)
	}

	// An unknown name errors.
	if _, err := runCmd(t, "--dir", dir, "secret", "rotate", "nope", "-y"); err == nil {
		t.Fatal("rotating an unknown secret should error")
	}
}

// TestCmdSecretCreateUsesPolicy: a secret with a generate policy is created
// non-interactively per the policy (no prompt, value not echoed); one without
// falls back to the interactive valuer.
func TestCmdSecretCreateUsesPolicy(t *testing.T) {
	dir := setupProject(t, testMain)
	created := map[string][]byte{}
	stubSecrets(t, nil, func(name string, value []byte, replace bool) error {
		created[name] = value
		return nil
	}, func(name string) ([]byte, bool, error) {
		return []byte("typed-" + name), false, nil // the interactive valuer
	})

	if _, err := runCmd(t, "--dir", dir, "secret", "add", "gen_pw", "--length", "48", "--charset", "hex"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCmd(t, "--dir", dir, "secret", "add", "manual_pw", "--manual"); err != nil {
		t.Fatal(err)
	}

	out, err := runCmd(t, "--dir", dir, "secret", "create", "gen_pw")
	if err != nil {
		t.Fatal(err)
	}
	if len(created["gen_pw"]) != 48 {
		t.Fatalf("policy secret should be 48 hex chars, got %d: %q", len(created["gen_pw"]), created["gen_pw"])
	}
	if strings.Contains(out, string(created["gen_pw"])) {
		t.Fatalf("a policy-generated value must not be echoed:\n%s", out)
	}

	// The manual entry has no policy, so it goes through the interactive valuer.
	if _, err := runCmd(t, "--dir", dir, "secret", "create", "manual_pw"); err != nil {
		t.Fatal(err)
	}
	if string(created["manual_pw"]) != "typed-manual_pw" {
		t.Fatalf("manual secret should use the valuer, got %q", created["manual_pw"])
	}
}

// TestCmdSecretAddCollision: a duplicate name is refused without --force.
func TestCmdSecretAddCollision(t *testing.T) {
	dir := setupProject(t, testMain)
	stubSecrets(t, nil, nil, nil)
	if _, err := runCmd(t, "--dir", dir, "secret", "add", "db"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCmd(t, "--dir", dir, "secret", "add", "db"); err == nil {
		t.Fatal("adding a duplicate should error without --force")
	}
	if _, err := runCmd(t, "--dir", dir, "secret", "add", "db", "--force", "--length", "50"); err != nil {
		t.Fatalf("--force should replace: %v", err)
	}
	entries, _ := eval.LoadSecretRegistry(dir, overlayFor(t, dir))
	if len(entries) != 1 || entries[0].Generate == nil || entries[0].Generate.Length != 50 {
		t.Fatalf("force-replace wrong: %+v", entries)
	}
}
