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
				"docker.io/a/young:v1":           "sha256:new",
			}[repoTag], nil
		},
		created: func(ref string) (time.Time, error) {
			// The young candidate is 1 day old; everything else is ancient.
			if strings.Contains(ref, "young") {
				return now.Add(-24 * time.Hour), nil
			}
			return now.Add(-100 * 24 * time.Hour), nil
		},
		// Only v1 exists anywhere: no tag advances, digest checks only.
		tags: func(repo string) ([]string, error) { return []string{"v1"}, nil },
	}
	entries := []eval.ImageEntry{
		{Key: "current", Image: "docker.io/a/managed-current:v1", Digest: "sha256:cur"},
		{Key: "behind", Image: "docker.io/a/managed-behind:v1", Digest: "sha256:old"},
		{Key: "young", Image: "docker.io/a/young:v1", Digest: "sha256:old", MinAge: "7d"},
		{Key: "unpinned", Image: "docker.io/a/x:v1"},
		{Key: "unmanaged", Image: "docker.io/a/y", Digest: "sha256:z"},
	}
	rows, available := checkOutdated(entries, 0, now, res)
	if available != 2 {
		t.Fatalf("available = %d, want 2 (behind + young; young is offered, just marked)", available)
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
	if !got["young"].update || !strings.Contains(got["young"].note, "younger than min-age") {
		t.Fatalf("young must be offered with the marker: %+v", got["young"])
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
	// Version tag advances implicitly (no range needed) and resolves the
	// new tag's digest; the candidate carries its age.
	e := eval.ImageEntry{Key: "x", Image: "docker.io/a/x:8.25.0", Digest: "sha256:old"}
	r := mustParse(t, e.Image)
	c, err := nextPin(e, r, 0, now, res)
	if err != nil {
		t.Fatal(err)
	}
	if c.Tag != "8.26.1" || c.Digest != "sha256:new" || c.Reason != "8.25.0 -> 8.26.1" || !c.HasAge || c.Young {
		t.Fatalf("implicit advance wrong: %+v", c)
	}

	// Min-age marks (never holds): the 1d-old candidate is Young under 7d.
	c, err = nextPin(e, r, 7*24*time.Hour, now, res)
	if err != nil {
		t.Fatal(err)
	}
	if c.Reason == "" || !c.Young {
		t.Fatalf("young candidate must be offered and marked: %+v", c)
	}

	// A frozen range suppresses the advance entirely.
	ef := eval.ImageEntry{Key: "x", Image: "docker.io/a/x:8.25.0", Digest: "sha256:old", Range: "=8.25.0"}
	c, err = nextPin(ef, r, 0, now, res)
	if err != nil {
		t.Fatal(err)
	}
	if c.Reason != "" {
		t.Fatalf("frozen entry must have no candidate: %+v", c)
	}

	// Float tag (not version-shaped): follows its own digest head.
	res.digest = func(string) (string, error) { return "sha256:head", nil }
	el := eval.ImageEntry{Key: "l", Image: "docker.io/a/x:latest", Digest: "sha256:old"}
	c, err = nextPin(el, mustParse(t, el.Image), 0, now, res)
	if err != nil {
		t.Fatal(err)
	}
	if c.Tag != "latest" || c.Digest != "sha256:head" || c.Reason != "digest moved" {
		t.Fatalf("float follow wrong: %+v", c)
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
