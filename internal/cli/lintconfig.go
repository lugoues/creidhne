package cli

import (
	"fmt"
	"sort"
	"strings"
)

// Rule severities. "off" disables a rule entirely; "error" fails validate
// (and exits lint non-zero even alone); "warn" reports.
const (
	sevError = "error"
	sevWarn  = "warn"
	sevOff   = "off"
)

// ruleDefaults registers every named rule crei enforces in Go, with its
// default severity. The [lint] table in crei.toml overrides per name:
//
//	[lint]
//	"graph/orphan-network" = "off"
//	"image/unpinned" = "error"
//
// (#checks defined in CUE are separate: they are schema constraints and fail
// evaluation; they cannot be softened here.)
var ruleDefaults = map[string]string{
	// Whole-project graph contracts (graphrules.go).
	"graph/pair-cardinality": sevError, // >1 external attacher on a pair network
	"graph/pair-unwired":     sevWarn,  // pair network missing proxy or service
	"graph/duplicate-name":   sevError, // effective runtime name collision
	"graph/orphan-network":   sevWarn,  // network no container/pod attaches
	"graph/duplicate-router": sevWarn,  // traefik router defined by two units
	// Redundant [Unit] dependencies (lint.go).
	"deps/redundant-resource":       sevWarn, // After/Requires/Wants duplicating a resource ref
	"deps/redundant-network-online": sevWarn, // hand-written network-online dep
	// Image registry (imagerules.go).
	"image/unpinned": sevWarn, // registry entry with a tag but no digest
	// Off by default: not using the registry is a supported choice ("if it's
	// not in a registry, crei never touches it"); flip to warn/error to be
	// nudged toward full registry coverage.
	"image/unmanaged": sevOff, // container image not from the registry
}

// lintLevels resolves rule names to effective severities: config overrides
// (validated against the registry) on top of the defaults.
type lintLevels map[string]string

// newLintLevels validates the [lint] config and merges it over the defaults.
// Unknown rule names and invalid severities are errors: a typo must fail
// loudly, not silently leave the intended rule at its default.
func newLintLevels(overrides map[string]string) (lintLevels, error) {
	levels := make(lintLevels, len(ruleDefaults))
	for name, sev := range ruleDefaults {
		levels[name] = sev
	}
	var unknown []string
	for name, sev := range overrides {
		if _, ok := ruleDefaults[name]; !ok {
			unknown = append(unknown, name)
			continue
		}
		switch sev {
		case sevError, sevWarn, sevOff:
			levels[name] = sev
		default:
			return nil, fmt.Errorf("[lint] %q: invalid severity %q (want %q, %q, or %q)", name, sev, sevError, sevWarn, sevOff)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("[lint] unknown rule(s): %s (known: %s)", strings.Join(unknown, ", "), strings.Join(knownRules(), ", "))
	}
	return levels, nil
}

func knownRules() []string {
	names := make([]string, 0, len(ruleDefaults))
	for n := range ruleDefaults {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// apply stamps each finding with its effective severity and drops disabled
// ones. Findings arrive with their rule name set and severity empty.
func (l lintLevels) apply(findings []ruleFinding) []ruleFinding {
	out := findings[:0:0]
	for _, f := range findings {
		sev, ok := l[f.Rule]
		if !ok {
			// A finding for an unregistered rule is a programming error;
			// surface it rather than dropping it.
			sev = sevError
		}
		if sev == sevOff {
			continue
		}
		f.Severity = sev
		out = append(out, f)
	}
	return out
}
