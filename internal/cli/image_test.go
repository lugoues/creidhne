package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/registry"
)

func TestCheckOutdated(t *testing.T) {
	now := time.Now()
	res := resolver{
		digest: func(repoTag string) (string, error) {
			return map[string]string{
				"docker.io/a/managed-current:v1": "sha256:cur",
				"docker.io/a/managed-behind:v1":  "sha256:new",
				"docker.io/a/held:v1":            "sha256:new",
			}[repoTag], nil
		},
		created: func(ref string) (time.Time, error) {
			// The held candidate is 1 day old; everything else is ancient.
			if strings.Contains(ref, "held") {
				return now.Add(-24 * time.Hour), nil
			}
			return now.Add(-100 * 24 * time.Hour), nil
		},
	}
	entries := []eval.ImageEntry{
		{Key: "current", Image: "docker.io/a/managed-current:v1", Digest: "sha256:cur"},
		{Key: "behind", Image: "docker.io/a/managed-behind:v1", Digest: "sha256:old"},
		{Key: "held", Image: "docker.io/a/held:v1", Digest: "sha256:old", MinAge: "7d"},
		{Key: "unpinned", Image: "docker.io/a/x:v1"},
		{Key: "unmanaged", Image: "docker.io/a/y", Digest: "sha256:z"},
	}
	rows, available := checkOutdated(entries, 0, now, res)
	if available != 1 {
		t.Fatalf("available = %d, want 1 (only 'behind')", available)
	}
	got := map[string]imageRow{}
	for _, r := range rows {
		got[r.name] = r
	}
	if !strings.Contains(got["current"].note, "up to date") {
		t.Fatalf("current: %q", got["current"].note)
	}
	if !got["behind"].update || !strings.Contains(got["behind"].note, "update available") {
		t.Fatalf("behind: %+v", got["behind"])
	}
	if got["held"].update || !strings.Contains(got["held"].note, "held") {
		t.Fatalf("held must not count as available: %+v", got["held"])
	}
	if got["unpinned"].status != "unpinned" || got["unmanaged"].status != "unmanaged" {
		t.Fatalf("classification wrong: %+v %+v", got["unpinned"], got["unmanaged"])
	}
}

func TestNextPin(t *testing.T) {
	now := time.Now()
	res := resolver{
		digest: func(repoTag string) (string, error) {
			return map[string]string{
				"docker.io/a/x:8.25.0": "sha256:old",
				"docker.io/a/x:8.26.1": "sha256:new",
			}[repoTag], nil
		},
		created: func(ref string) (time.Time, error) { return now.Add(-24 * time.Hour), nil },
		tags:    func(repo string) ([]string, error) { return []string{"8.25.0", "8.26.1", "latest"}, nil },
	}
	// Range advances the tag and resolves the new tag's digest.
	e := eval.ImageEntry{Key: "x", Image: "docker.io/a/x:8.25.0", Digest: "sha256:old", Range: "^8"}
	r := mustParse(t, e.Image)
	c, err := nextPin(e, r, 0, now, res)
	if err != nil {
		t.Fatal(err)
	}
	if c.Tag != "8.26.1" || c.Digest != "sha256:new" || !strings.Contains(c.Reason, "tag 8.25.0 -> 8.26.1") || c.Held != "" {
		t.Fatalf("range advance wrong: %+v", c)
	}

	// Min-age holds the young candidate.
	c, err = nextPin(e, r, 7*24*time.Hour, now, res)
	if err != nil {
		t.Fatal(err)
	}
	if c.Held == "" {
		t.Fatalf("young candidate must be held: %+v", c)
	}

	// No range, digest current: no reason.
	e2 := eval.ImageEntry{Key: "x", Image: "docker.io/a/x:8.25.0", Digest: "sha256:old"}
	c, err = nextPin(e2, mustParse(t, e2.Image), 0, now, res)
	if err != nil {
		t.Fatal(err)
	}
	if c.Reason != "" {
		t.Fatalf("current entry must have no reason: %+v", c)
	}
}

func mustParse(t *testing.T, img string) registry.Ref {
	t.Helper()
	r, err := registry.Parse(img)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDeriveName(t *testing.T) {
	cases := map[string]string{
		"ghcr.io/matrix-construct/tuwunel":      "tuwunel",
		"ghcr.io/home-assistant/home-assistant": "home_assistant",
		"docker.io/library/redis":               "redis",
		"ghcr.io/paperless-ngx/paperless-ngx":   "paperless_ngx",
		"docker.io/company/7zip":                "_7zip",
		"registry:5000/team/my.app":             "my_app",
	}
	for repo, want := range cases {
		if got := deriveName(repo); got != want {
			t.Fatalf("deriveName(%q) = %q, want %q", repo, got, want)
		}
	}
}

func TestEmitImageRegistry(t *testing.T) {
	entries := []eval.ImageEntry{
		{Key: "gluetun", Image: "docker.io/qmcgaw/gluetun:v3", Digest: "sha256:abc"},
		{Key: "ha", Image: "ghcr.io/x/home-assistant:stable", Digest: "sha256:def", MinAge: "3d"},
		{Key: "fresh", Image: "docker.io/x/y:1"},
		{Key: "ranged", Image: "docker.io/x/z:8.25.0", Digest: "sha256:r", Range: "~8.25"},
	}
	out, err := emitImageRegistry(entries)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"package registries",
		`import "github.com/lugoues/creidhne"`,
		"images: creidhne.#ImageRegistry & {",
		`gluetun: {image: "docker.io/qmcgaw/gluetun:v3", digest: "sha256:abc"}`,
		`ha: {image: "ghcr.io/x/home-assistant:stable", digest: "sha256:def", minAge: "3d"}`,
		`fresh: image: "docker.io/x/y:1"`,
		`ranged: {image: "docker.io/x/z:8.25.0", digest: "sha256:r", range: "~8.25"}`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("emit missing %q:\n%s", want, s)
		}
	}
}
