package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cuelang.org/go/cue/load"

	"github.com/lugoues/creidhne"
	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/registry"
)

// writeProject materializes a minimal CUE project whose registries/images.cue
// is emitImageRegistry's own output, so a round-trip test exercises the real
// writer rather than a hand-written approximation of it.
func writeProject(t *testing.T, dir string, entries []eval.ImageEntry) {
	t.Helper()
	content, err := emitImageRegistry(entries)
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "cue.mod", "module.cue"),
		"module: \"example.com/demo@v0\"\nlanguage: version: \"v0.17.0\"\n")
	write(filepath.Join(dir, "registries", "images.cue"), string(content))
}

// overlayFor resolves the embedded schema for dir, offline.
func overlayFor(t *testing.T, dir string) map[string]load.Source {
	t.Helper()
	o, err := eval.Overlay(dir, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

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
	rows, available, _ := checkOutdated(entries, 0, now, res)
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
		{Key: "locked", Image: "docker.io/library/traefik:v3.6", Digest: "sha256:t",
			Lock: &eval.ImageLock{Reason: "3.6 breaks websocket upgrades", Since: "2026-07-24"}},
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
		// A lock expands the entry: the reason is prose meant to be read.
		`reason: "3.6 breaks websocket upgrades"`,
		`since:  "2026-07-24"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("emit missing %q:\n%s", want, s)
		}
	}
	// CUE indents with tabs; a hand-built emitter that leaks spaces would make
	// crei-written files fail `cue fmt`. format.Source guarantees tabs, so no
	// indented line may begin with a space.
	for i, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, " ") {
			t.Fatalf("line %d is space-indented, want tabs: %q", i+1, line)
		}
	}
}

// TestEmitDoesNotReorderCaller: emit must not sort the caller's slice. Callers
// hold indexes into it (findImage's result, the picker's updateItem.idx) and
// used across a write those indexes would silently address a different entry --
// which reported the wrong image on `crei image lock` until emit sorted a copy.
func TestEmitDoesNotReorderCaller(t *testing.T) {
	entries := []eval.ImageEntry{
		{Key: "zulu", Image: "docker.io/x/zulu:1", Digest: "sha256:abc"},
		{Key: "alpha", Image: "docker.io/x/alpha:1", Digest: "sha256:def"},
	}
	if _, err := emitImageRegistry(entries); err != nil {
		t.Fatal(err)
	}
	if entries[0].Key != "zulu" || entries[1].Key != "alpha" {
		t.Fatalf("emit reordered the caller's slice: %q, %q", entries[0].Key, entries[1].Key)
	}
}

// TestLockRoundTrip: a locked entry survives emit -> load unchanged. The
// registry file is crei-rewritten wholesale on every pin/update, so a field the
// emitter drops is a field silently deleted from the user's config.
func TestLockRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Digests must be real hex: this loads through the schema, whose regex
	// rejects anything else.
	writeProject(t, dir, []eval.ImageEntry{
		{Key: "traefik", Image: "docker.io/library/traefik:v3.6", Digest: "sha256:beef",
			Lock: &eval.ImageLock{Reason: "3.6 breaks websocket upgrades", Since: "2026-07-24"}},
		{Key: "plain", Image: "docker.io/x/y:1", Digest: "sha256:cafe"},
	})

	got, err := eval.LoadImageRegistry(dir, overlayFor(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]eval.ImageEntry{}
	for _, e := range got {
		byKey[e.Key] = e
	}
	switch l := byKey["traefik"].Lock; {
	case l == nil:
		t.Fatal("traefik lost its lock through emit -> load")
	case l.Reason != "3.6 breaks websocket upgrades" || l.Since != "2026-07-24":
		t.Fatalf("lock round-tripped wrong: %+v", l)
	}
	if byKey["plain"].Lock != nil {
		t.Fatalf("plain must stay unlocked, got %+v", byKey["plain"].Lock)
	}
}

// TestUpdateSkipsLocked: the point of the feature. A locked entry with a
// waiting update is reported as held, never offered for selection, and is not
// rewritten.
func TestUpdateSkipsLocked(t *testing.T) {
	now := time.Now()
	res := resolver{
		digest:  func(string) (string, error) { return "sha256:new", nil },
		created: func(string) (time.Time, error) { return now.Add(-100 * 24 * time.Hour), nil },
		tags:    func(string) ([]string, error) { return []string{"v1"}, nil },
	}
	locked := eval.ImageEntry{Key: "traefik", Image: "docker.io/a/traefik:v1", Digest: "sha256:old",
		Lock: &eval.ImageLock{Reason: "3.6 breaks websockets", Since: "2026-01-01"}}
	free := eval.ImageEntry{Key: "other", Image: "docker.io/a/other:v1", Digest: "sha256:old"}

	// checkOutdated: locked is reported, with what it holds back, and does not
	// count as an available update (so outdated stays exit-zero).
	rows, available, _ := checkOutdated([]eval.ImageEntry{locked}, 0, now, res)
	if available != 0 {
		t.Fatalf("a locked entry must not count as available, got %d", available)
	}
	if rows[0].status != "locked" {
		t.Fatalf("status = %q, want locked", rows[0].status)
	}
	for _, want := range []string{"3.6 breaks websockets", "holding back"} {
		if !strings.Contains(rows[0].note, want) {
			t.Fatalf("note %q missing %q", rows[0].note, want)
		}
	}

	// The picker's own split: locked candidates go to held, never to items.
	items, held, _ := splitCandidates([]eval.ImageEntry{locked, free}, nil, 0, now, res)
	if len(items) != 1 || items[0].e.Key != "other" {
		t.Fatalf("selectable = %+v, want only 'other'", items)
	}
	if len(held) != 1 || held[0].e.Key != "traefik" {
		t.Fatalf("held = %+v, want only 'traefik'", held)
	}

	// Naming a locked entry does not override the lock, unlike a min-age
	// marker. This is the load-bearing difference between the two.
	items, held, _ = splitCandidates([]eval.ImageEntry{locked}, map[string]bool{"traefik": true}, 0, now, res)
	if len(items) != 0 {
		t.Fatalf("naming a locked entry must not offer it, got %+v", items)
	}
	if len(held) != 1 {
		t.Fatalf("naming a locked entry should still report it held, got %+v", held)
	}
}

func TestLockAge(t *testing.T) {
	at := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		since   string
		wantOK  bool
		wantDur time.Duration
	}{
		{"2026-07-14", true, 10 * 24 * time.Hour},
		{"", false, 0},
		{"not-a-date", false, 0}, // hand-edited garbage degrades quietly
		{"2027-01-01", true, 0},  // a future date reads as just-placed
	}
	for _, c := range cases {
		got, ok := lockAge(&eval.ImageLock{Reason: "x", Since: c.since}, at)
		if ok != c.wantOK || got != c.wantDur {
			t.Fatalf("lockAge(%q) = %v,%v; want %v,%v", c.since, got, ok, c.wantDur, c.wantOK)
		}
	}
}
