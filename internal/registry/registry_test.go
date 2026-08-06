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
		{"docker.io/x/y:v3@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			"docker.io/x/y", "v3", "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
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
	// A malformed tag or digest must fail up front: written back to
	// registries/images.cue it would wedge every later load on schema
	// validation.
	for _, bad := range []string{
		"docker.io/x/y:v1@sha256:nothex",
		"docker.io/x/y:v1@sha256:abc",
		"docker.io/x/y:not a tag",
	} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("Parse(%q) must error", bad)
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

func TestPickVersion(t *testing.T) {
	mustV := func(tag string) Version {
		v, ok := ParseVersion(tag)
		if !ok {
			t.Fatalf("ParseVersion(%q) failed", tag)
		}
		return v
	}

	tags := []string{"latest", "stable", "8.25.0", "8.25.3", "8.26.1", "v9.0.0", "sha-abc123", "8"}
	cases := []struct{ current, constraint, want string }{
		{"8.25.0", "~8.25", "8.25.3"},
		{"8.25.0", "^8.25", "8.26.1"},
		{"8.25.0", "", "v9.0.0"},  // implicit >= current: newest overall
		{"8.25.0", "=8.25.0", ""}, // frozen: nothing strictly newer in range
		{"8.26.1", "~8.25", ""},   // already past the range: no downgrade
		{"v9.0.0", "", ""},        // newest already
	}
	for _, c := range cases {
		got, err := PickVersion(tags, mustV(c.current), c.constraint)
		if err != nil {
			t.Fatalf("PickVersion(cur=%q, %q): %v", c.current, c.constraint, err)
		}
		if got != c.want {
			t.Fatalf("PickVersion(cur=%q, %q) = %q, want %q", c.current, c.constraint, got, c.want)
		}
	}
	if _, err := PickVersion(tags, mustV("1.0"), "not a range ]["); err == nil {
		t.Fatal("invalid constraint must error")
	}

	// CalVer with a 4-part hotfix ranks above its 3-part base (the hermes
	// shape); suffixes are compatibility and never cross.
	calTags := []string{"v2026.7.7", "v2026.7.7.2", "v2026.6.19", "main", "latest"}
	if got, _ := PickVersion(calTags, mustV("v2026.6.19"), ""); got != "v2026.7.7.2" {
		t.Fatalf("CalVer hotfix pick = %q, want v2026.7.7.2", got)
	}
	// Alias-narrowing guard: a deeper tag refining the same version is the
	// same image, not an upgrade (debian:13-slim vs 13.6-slim); a genuinely
	// newer major still advances the alias.
	debTags := []string{"13-slim", "13.6-slim", "13.5-slim", "12-slim"}
	if got, _ := PickVersion(debTags, mustV("13-slim"), ""); got != "" {
		t.Fatalf("alias refinement must not be offered, got %q", got)
	}
	if got, _ := PickVersion(append(debTags, "14-slim"), mustV("13-slim"), ""); got != "14-slim" {
		t.Fatalf("major alias advance = %q, want 14-slim", got)
	}

	// Scheme guards: date-stamp tags never beat dotted versions (alpine
	// publishes both), and less-precise tags are a different scheme.
	alpTags := []string{"3.20", "3.20.3", "3.21", "20260127", "edge", "latest"}
	if got, _ := PickVersion(alpTags, mustV("3.20"), ""); got != "3.21" {
		t.Fatalf("alpine pick = %q, want 3.21 (date tags excluded)", got)
	}
	if got, _ := PickVersion([]string{"9", "8.26", "20260127"}, mustV("8.25.0"), ""); got != "" {
		t.Fatalf("less-precise candidates must be excluded, got %q", got)
	}

	sufTags := []string{"1.2.0-alpine", "1.2.1-alpine", "1.3.0", "1.3.0-stretch"}
	if got, _ := PickVersion(sufTags, mustV("1.2.0-alpine"), ""); got != "1.2.1-alpine" {
		t.Fatalf("suffix pick = %q, want 1.2.1-alpine (never cross-suffix)", got)
	}
	if got, _ := PickVersion(sufTags, mustV("1.2.0"), ""); got != "1.3.0" {
		t.Fatalf("bare pick = %q, want 1.3.0 (suffixed excluded)", got)
	}
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2026.7.7.2", "2026.7.7", 1},
		{"8.25", "8.25.0", 0},
		{"v9", "8.99.99", 1},
		{"1.2.3", "1.10.0", -1},
	}
	for _, c := range cases {
		a, _ := ParseVersion(c.a)
		b, _ := ParseVersion(c.b)
		if got := a.Compare(b); got != c.want {
			t.Fatalf("Compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	for _, bad := range []string{"latest", "main", "sha-abc", "1.2.x", ""} {
		if _, ok := ParseVersion(bad); ok {
			t.Fatalf("ParseVersion(%q) must fail", bad)
		}
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
