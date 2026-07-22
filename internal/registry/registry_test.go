package registry

import (
	"os"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in                       string
		repo, tag, digest string
		status                   Status
	}{
		{"docker.io/qmcgaw/gluetun:v3@sha256:ad6b", "docker.io/qmcgaw/gluetun", "v3", "sha256:ad6b", Managed},
		{"ghcr.io/home-assistant/home-assistant:stable", "ghcr.io/home-assistant/home-assistant", "stable", "", Unpinned},
		{"docker.io/ttlequals0/minuspod@sha256:65ce", "docker.io/ttlequals0/minuspod", "", "sha256:65ce", Unmanaged},
		{"registry:5000/team/app:1.2@sha256:abc", "registry:5000/team/app", "1.2", "sha256:abc", Managed},
		{"docker.io/gotenberg/gotenberg:8.25", "docker.io/gotenberg/gotenberg", "8.25", "", Unpinned},
	}
	for _, c := range cases {
		r, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.in, err)
		}
		if r.Repo != c.repo || r.Tag != c.tag || r.Digest != c.digest || r.Status() != c.status {
			t.Fatalf("Parse(%q) = %+v status=%s, want repo=%q tag=%q digest=%q status=%s",
				c.in, r, r.Status(), c.repo, c.tag, c.digest, c.status)
		}
	}
}

func TestPinned(t *testing.T) {
	r, _ := Parse("docker.io/qmcgaw/gluetun:v3@sha256:old")
	if got := r.Pinned("sha256:new"); got != "docker.io/qmcgaw/gluetun:v3@sha256:new" {
		t.Fatalf("Pinned = %q", got)
	}
	if got := r.TaggedRef(); got != "docker.io/qmcgaw/gluetun:v3" {
		t.Fatalf("TaggedRef = %q", got)
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
