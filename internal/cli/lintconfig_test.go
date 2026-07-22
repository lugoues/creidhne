package cli

import (
	"strings"
	"testing"

	"github.com/lugoues/creidhne/internal/eval"
)

func TestLintLevels(t *testing.T) {
	// Defaults pass through.
	levels, err := newLintLevels(nil)
	if err != nil {
		t.Fatal(err)
	}
	fs := levels.apply([]ruleFinding{
		{Rule: "graph/duplicate-name", Unit: "a"},
		{Rule: "graph/orphan-network", Unit: "b"},
		{Rule: "image/unmanaged", Unit: "c"}, // off by default: dropped
	})
	if len(fs) != 2 || fs[0].Severity != sevError || fs[1].Severity != sevWarn {
		t.Fatalf("default apply wrong: %+v", fs)
	}

	// Overrides: downgrade an error, enable an off rule, disable a warn.
	levels, err = newLintLevels(map[string]string{
		"graph/duplicate-name": "warn",
		"image/unmanaged":      "error",
		"graph/orphan-network": "off",
	})
	if err != nil {
		t.Fatal(err)
	}
	fs = levels.apply([]ruleFinding{
		{Rule: "graph/duplicate-name", Unit: "a"},
		{Rule: "graph/orphan-network", Unit: "b"},
		{Rule: "image/unmanaged", Unit: "c"},
	})
	if len(fs) != 2 {
		t.Fatalf("override apply wrong: %+v", fs)
	}
	got := map[string]string{}
	for _, f := range fs {
		got[f.Rule] = f.Severity
	}
	if got["graph/duplicate-name"] != sevWarn || got["image/unmanaged"] != sevError {
		t.Fatalf("override severities wrong: %v", got)
	}

	// Unknown rule and invalid severity fail loudly.
	if _, err := newLintLevels(map[string]string{"graph/orphan-netwrk": "off"}); err == nil || !strings.Contains(err.Error(), "unknown rule") {
		t.Fatalf("typo must error, got %v", err)
	}
	if _, err := newLintLevels(map[string]string{"graph/orphan-network": "silent"}); err == nil || !strings.Contains(err.Error(), "invalid severity") {
		t.Fatalf("bad severity must error, got %v", err)
	}
}

func TestImageRuleFindings(t *testing.T) {
	entries := []eval.ImageEntry{
		{Key: "pinned", Image: "docker.io/a/x:v1", Digest: "sha256:abc"},
		{Key: "loose", Image: "docker.io/a/y:v1"},
	}
	quads := []eval.Quadlet{{Name: "app", Units: []eval.UnitRecord{
		{Kind: "container", Filename: "managed.container", Data: map[string]any{"imageString": "docker.io/a/x:v1@sha256:abc"}},
		{Kind: "container", Filename: "loose.container", Data: map[string]any{"imageString": "docker.io/a/y:v1"}},
		{Kind: "container", Filename: "inline.container", Data: map[string]any{"imageString": "docker.io/raw/thing:latest"}},
		{Kind: "container", Filename: "built.container", Data: map[string]any{"imageString": "app.build"}},
	}}}

	fs := imageRuleFindings(quads, entries)
	byRule := map[string][]string{}
	for _, f := range fs {
		byRule[f.Rule] = append(byRule[f.Rule], f.Unit+": "+f.Message)
	}
	if n := len(byRule["image/unpinned"]); n != 1 || !strings.Contains(byRule["image/unpinned"][0], "loose") {
		t.Fatalf("unpinned findings wrong: %v", byRule["image/unpinned"])
	}
	// Only the raw inline image is unmanaged: the managed ref, the unpinned
	// entry's bare ref, and the .build self-ref are all covered.
	if n := len(byRule["image/unmanaged"]); n != 1 || !strings.Contains(byRule["image/unmanaged"][0], "inline.container") {
		t.Fatalf("unmanaged findings wrong: %v", byRule["image/unmanaged"])
	}
}
