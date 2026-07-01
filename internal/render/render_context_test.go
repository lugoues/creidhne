package render

import (
	"strings"
	"testing"

	"github.com/lugoues/creidhne/internal/eval"
)

// TestBuildContextKeyIsCleaned guards the idempotency bug where an absolute (or
// otherwise unclean) context key produced a desired-set path with a double slash
// ("images/<stem>.context//home/x") that never matched the cleaned path the
// filesystem stores, so every apply oscillated add<->remove. The emitted key
// must be the canonical relative path, and its mode must survive.
func TestBuildContextKeyIsCleaned(t *testing.T) {
	r := newTestRenderer(t)
	bu := eval.UnitRecord{
		Kind: "build", Stem: "hermes", Filename: "hermes.build",
		Data: map[string]any{
			"ContainerFile": "FROM scratch\n",
			"Context": map[string]any{
				"/home/hermes/.local/bin/hermes-gateways": map[string]any{"content": "#!/bin/sh\n", "mode": "0770"},
				"a//b/./c": "x",
			},
		},
	}
	files, err := r.BuildFileSet([]eval.Quadlet{{Name: "hermes", Units: []eval.UnitRecord{bu}}})
	if err != nil {
		t.Fatal(err)
	}
	for k := range files {
		if strings.Contains(k, "//") {
			t.Fatalf("emitted a non-canonical key with a double slash: %q", k)
		}
	}
	want := "images/hermes.context/home/hermes/.local/bin/hermes-gateways"
	fc, ok := files[want]
	if !ok {
		keys := make([]string, 0, len(files))
		for k := range files {
			keys = append(keys, k)
		}
		t.Fatalf("want canonical key %q, got %v", want, keys)
	}
	if fc.Mode != "0770" {
		t.Fatalf("mode = %q, want 0770", fc.Mode)
	}
	if _, ok := files["images/hermes.context/a/b/c"]; !ok {
		t.Fatal("want cleaned key images/hermes.context/a/b/c")
	}
}
