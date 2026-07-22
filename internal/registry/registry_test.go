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
