package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/lugoues/creidhne/internal/eval"
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
		{Key: "current", Ref: "docker.io/a/managed-current:v1@sha256:cur"},
		{Key: "behind", Ref: "docker.io/a/managed-behind:v1@sha256:old"},
		{Key: "held", Ref: "docker.io/a/held:v1@sha256:old", MinAge: "7d"},
		{Key: "unpinned", Ref: "docker.io/a/x:v1"},
		{Key: "unmanaged", Ref: "docker.io/a/y@sha256:z"},
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

func TestEmitImageRegistry(t *testing.T) {
	entries := []eval.ImageEntry{
		{Key: "gluetun", Ref: "docker.io/qmcgaw/gluetun:v3@sha256:abc"},
		{Key: "ha", Ref: "ghcr.io/x/home-assistant:stable@sha256:def", MinAge: "3d"},
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
		`gluetun: ref: "docker.io/qmcgaw/gluetun:v3@sha256:abc"`,
		`ha: {ref: "ghcr.io/x/home-assistant:stable@sha256:def", minAge: "3d"}`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("emit missing %q:\n%s", want, s)
		}
	}
}
