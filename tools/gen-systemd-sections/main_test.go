package main

import (
	"os"
	"strings"
	"testing"
)

func names(ds []directive) map[string]bool {
	m := make(map[string]bool, len(ds))
	for _, d := range ds {
		m[d.name] = true
	}
	return m
}

func typesByName(ds []directive) map[string]string {
	m := make(map[string]string, len(ds))
	for _, d := range ds {
		m[d.name] = d.cueType
	}
	return m
}

// TestParseSectionRealGperfInvariants runs the parser over the actual vendored
// gperf and asserts the properties that the generated schema's correctness rests
// on, the ones that the golden tests, cue vet and the gen-drift check do NOT
// cover. In particular it guards the dedup regression: many distinct directives
// share a generic context-struct offset (config_parse_memory_limit, exec_output,
// etc.), and an earlier target-based dedup silently dropped all but the first.
// These are presence/absence checks (not brittle counts), so they survive a
// systemd version bump unless a directive genuinely disappears.
func TestParseSectionRealGperfInvariants(t *testing.T) {
	raw, err := os.ReadFile("load-fragment-gperf.gperf.in")
	if err != nil {
		t.Fatalf("read vendored gperf: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	unmapped := map[string]bool{}

	svc := names(parseSection(lines, "Service", unmapped))
	unit := names(parseSection(lines, "Unit", unmapped))

	// Distinct [Service] directives that share a context-struct offset and were
	// dropped by the old dedup, but all must survive (the bug that shipped silently).
	for _, d := range []string{
		"MemoryMin", "MemoryLow", "MemoryHigh", "MemoryMax", "MemorySwapMax",
		"StandardOutput", "StandardError",
		"ReadWritePaths", "ReadOnlyPaths", "InaccessiblePaths",
		"BindPaths", "BindReadOnlyPaths",
		"IOReadBandwidthMax", "IOWriteBandwidthMax",
	} {
		if !svc[d] {
			t.Errorf("[Service] missing %q (regression: distinct directive dropped)", d)
		}
	}

	// Legacy spellings are dropped in favor of the canonical name.
	for _, d := range []string{"ReadWriteDirectories", "ReadOnlyDirectories", "InaccessibleDirectories"} {
		if svc[d] {
			t.Errorf("[Service] should drop legacy alias %q", d)
		}
	}
	// Legacy [Service] locations of [Unit] directives are dropped (canonical is
	// generated into #UnitSection).
	for _, d := range []string{"FailureAction", "RebootArgument", "StartLimitAction", "StartLimitBurst", "StartLimitInterval"} {
		if svc[d] {
			t.Errorf("[Service] should drop legacy-location %q (canonical lives in [Unit])", d)
		}
	}

	// [Unit] keeps the canonical dependency names and drops their legacy spellings.
	for _, d := range []string{"BindsTo", "PropagatesReloadTo", "ReloadPropagatedFrom", "StartLimitIntervalSec"} {
		if !unit[d] {
			t.Errorf("[Unit] missing canonical %q", d)
		}
	}
	for _, d := range []string{"BindTo", "PropagateReloadTo", "PropagateReloadFrom", "StartLimitInterval"} {
		if unit[d] {
			t.Errorf("[Unit] should drop legacy alias %q", d)
		}
	}
}

// TestParseSectionSynthetic exercises the parsing edge cases in isolation on a
// crafted snippet: macro expansion, the type mapping, deprecated-parser skip,
// unmapped-parser fallback, cross-section isolation, and (the key one) two
// distinct directives sharing an identical parser+target both surviving (no
// dedup), which is exactly the class the old code got wrong.
func TestParseSectionSynthetic(t *testing.T) {
	lines := []string{
		"{%- macro MYCTX(type) -%}",
		"{{type}}.MacroField, config_parse_bool, 0, offsetof(X, ctx)",
		"{%- endmacro -%}",
		"Test.FieldA, config_parse_string, 0, offsetof(X, ctx)",
		"Test.FieldB, config_parse_string, 0, offsetof(X, ctx)", // same parser+target as A
		"Test.Repeatable, config_parse_strv, 0, 0",
		"Test.Deprecated, config_parse_warn_compat, DISABLED_LEGACY, 0",
		"Test.Mystery, config_parse_totally_unknown_xyz, 0, 0",
		"{{ MYCTX('Test') }}",
		"Other.Ignored, config_parse_bool, 0, 0",
	}
	unmapped := map[string]bool{}
	got := typesByName(parseSection(lines, "Test", unmapped))

	want := map[string]string{
		"MacroField": "bool",        // expanded from the macro invocation
		"FieldA":     "string",      // both distinct directives sharing one
		"FieldB":     "string",      // parser+target survive (no dedup)
		"Repeatable": "[...string]", // config_parse_strv
		"Mystery":    "string",      // unmapped parser falls back to string
	}
	for name, wantType := range want {
		if got[name] != wantType {
			t.Errorf("directive %q: type %q, want %q", name, got[name], wantType)
		}
	}
	if _, ok := got["Deprecated"]; ok {
		t.Error("deprecated parser (config_parse_warn_compat) should be skipped")
	}
	if _, ok := got["Ignored"]; ok {
		t.Error("a different section's directive must not leak in")
	}
	if !unmapped["config_parse_totally_unknown_xyz"] {
		t.Error("an unmapped parser should be recorded for the stderr report")
	}
}

func TestRenderKind(t *testing.T) {
	cases := map[string]string{
		"[...string]": "list",
		"uint":        "int",
		"int":         "int",
		"string":      "scalar",
		"#Bytes":      "scalar",
		"bool":        "scalar",
	}
	for in, want := range cases {
		if got := renderKind(in); got != want {
			t.Errorf("renderKind(%q) = %q, want %q", in, got, want)
		}
	}
}
