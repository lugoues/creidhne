package registry

import (
	"os"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in                string
		repo, tag, digest string
	}{
		// image-form inputs (no digest — the registry's `image` field)
		{"docker.io/qmcgaw/gluetun:v3", "docker.io/qmcgaw/gluetun", "v3", ""},
		{"ghcr.io/home-assistant/home-assistant:stable", "ghcr.io/home-assistant/home-assistant", "stable", ""},
		{"docker.io/ttlequals0/minuspod", "docker.io/ttlequals0/minuspod", "", ""},
		{"registry:5000/team/app:1.2", "registry:5000/team/app", "1.2", ""},
		// combined form still splits (Parse is form-agnostic)
		{"docker.io/x/y:v3@sha256:abc", "docker.io/x/y", "v3", "sha256:abc"},
	}
	for _, c := range cases {
		r, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.in, err)
		}
		if r.Repo != c.repo || r.Tag != c.tag || r.Digest != c.digest {
			t.Fatalf("Parse(%q) = %+v, want repo=%q tag=%q digest=%q", c.in, r, c.repo, c.tag, c.digest)
		}
	}
	if got := (Ref{Repo: "docker.io/x/y", Tag: "v3"}).TaggedRef(); got != "docker.io/x/y:v3" {
		t.Fatalf("TaggedRef = %q", got)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		hasTag, hasDigest bool
		want              Status
	}{
		{true, true, Managed},
		{true, false, Unpinned},
		{false, true, Unmanaged},
		{false, false, Unmanaged},
	}
	for _, c := range cases {
		if got := Classify(c.hasTag, c.hasDigest); got != c.want {
			t.Fatalf("Classify(%v,%v) = %s, want %s", c.hasTag, c.hasDigest, got, c.want)
		}
	}
}

func TestParseAge(t *testing.T) {
	for in, want := range map[string]float64{"12h": 12, "1d": 24, "2w": 336} {
		d, err := ParseAge(in)
		if err != nil || d.Hours() != want {
			t.Fatalf("ParseAge(%q) = %v, %v; want %v h", in, d, err, want)
		}
	}
	if _, err := ParseAge("3x"); err == nil {
		t.Fatal("ParseAge(3x) must error")
	}
}

// TestPodmanAuthFallback covers the sudo/root gap: podman's default auth
// locations that the crane keychain never checks on its own.
func TestPodmanAuthFallback(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	exists := func(paths ...string) func(string) bool {
		set := map[string]bool{}
		for _, p := range paths {
			set[p] = true
		}
		return func(p string) bool { return set[p] }
	}

	// Explicit REGISTRY_AUTH_FILE: leave alone.
	if got := podmanAuthFallback(env(map[string]string{"REGISTRY_AUTH_FILE": "/x"}), 0, exists()); got != "" {
		t.Fatalf("explicit REGISTRY_AUTH_FILE must not be overridden, got %q", got)
	}
	// XDG set and auth present: keychain already finds it, leave alone.
	if got := podmanAuthFallback(env(map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"}), 1000,
		exists("/run/user/1000/containers/auth.json")); got != "" {
		t.Fatalf("XDG-visible auth must not be overridden, got %q", got)
	}
	// sudo shape: no env, uid 0, root podman auth present -> found.
	if got := podmanAuthFallback(env(nil), 0, exists("/run/containers/0/auth.json")); got != "/run/containers/0/auth.json" {
		t.Fatalf("root podman auth not found, got %q", got)
	}
	// stripped env for a user: per-uid runtime dir still found.
	if got := podmanAuthFallback(env(nil), 1000, exists("/run/user/1000/containers/auth.json")); got != "/run/user/1000/containers/auth.json" {
		t.Fatalf("per-uid podman auth not found, got %q", got)
	}
	// nothing anywhere: stay anonymous.
	if got := podmanAuthFallback(env(nil), 1000, exists()); got != "" {
		t.Fatalf("no auth anywhere must return empty, got %q", got)
	}
}

// TestDigestReal resolves a live digest. Network-gated (CREI_TEST_REGISTRY) so
// CI/offline runs skip it, like the podman integration test.
func TestDigestReal(t *testing.T) {
	if os.Getenv("CREI_TEST_REGISTRY") == "" {
		t.Skip("set CREI_TEST_REGISTRY to hit a real registry")
	}
	d, err := Digest("docker.io/library/alpine:latest")
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if len(d) < 20 || d[:7] != "sha256:" {
		t.Fatalf("unexpected digest %q", d)
	}
}
