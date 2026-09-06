package cli

import (
	"strings"
	"testing"
)

// TestStopTimeoutExplicitConflict: explicit TimeoutStopSec at or below an
// explicit StopTimeout is an error; above it is clean.
func TestStopTimeoutExplicitConflict(t *testing.T) {
	proj := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
bad: creidhne.#Quadlet & {name: "bad", units: #container: {
	Container: {Image: "docker.io/x", StopTimeout: 120}
	Service: TimeoutStopSec: "2min"
}}
`)
	out, err := runCmd(t, "--dir", proj, "validate")
	if err == nil || !strings.Contains(out, "service/stop-timeout") || !strings.Contains(out, "TimeoutStopSec (120s) does not exceed StopTimeout (120s)") {
		t.Fatalf("equal timeouts must fail validate: %v\n%s", err, out)
	}

	proj = setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
ok: creidhne.#Quadlet & {name: "ok", units: #container: {
	Container: {Image: "docker.io/x", StopTimeout: 120}
	Service: TimeoutStopSec: "3min"
}}
inf: creidhne.#Quadlet & {name: "inf", units: #container: {
	Container: {Image: "docker.io/y", StopTimeout: 300}
	Service: TimeoutStopSec: "infinity"
}}
zero: creidhne.#Quadlet & {name: "zero", units: #container: {
	Container: {Image: "docker.io/z", StopTimeout: 300}
	Service: TimeoutStopSec: "0"
}}
`)
	if out, err := runCmd(t, "--dir", proj, "validate"); err != nil {
		t.Fatalf("coherent timeouts must pass: %v\n%s", err, out)
	}
}

// TestStopTimeoutDefaultHeuristics: one side explicit, compared against the
// assumed defaults (grace 10s, DefaultTimeoutStopSec 90s): warns, still valid.
func TestStopTimeoutDefaultHeuristics(t *testing.T) {
	proj := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
longgrace: creidhne.#Quadlet & {name: "longgrace", units: #container: Container: {Image: "docker.io/x", StopTimeout: 90}}
shortstop: creidhne.#Quadlet & {name: "shortstop", units: #pod: {Pod: {}, Service: TimeoutStopSec: "5"}}
`)
	out, err := runCmd(t, "--dir", proj, "validate")
	if err != nil {
		t.Fatalf("default-heuristic findings are warnings, validate must pass: %v\n%s", err, out)
	}
	for _, want := range []string{
		"StopTimeout (90s) meets or exceeds systemd's default stop timeout",
		"TimeoutStopSec (5s) does not exceed podman's default grace period",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing warning %q:\n%s", want, out)
		}
	}
}

// TestStopTimeoutPodmanArgsOverride: --stop-timeout in a container's
// PodmanArgs is the effective grace period.
func TestStopTimeoutPodmanArgsOverride(t *testing.T) {
	proj := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
a: creidhne.#Quadlet & {name: "a", units: #container: {
	Container: {Image: "docker.io/x", PodmanArgs: ["--stop-timeout=\"45\""]}
	Service: TimeoutStopSec: "30"
}}
`)
	out, err := runCmd(t, "--dir", proj, "validate")
	if err == nil || !strings.Contains(out, "StopTimeout (45s)") {
		t.Fatalf("PodmanArgs --stop-timeout must be honored: %v\n%s", err, out)
	}
}

func TestParseTimeSpan(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"90", 90, true},
		{"90s", 90, true},
		{"1min 30s", 90, true},
		{"5min20s", 320, true},
		{"1.5h", 5400, true},
		{"250ms", 0.25, true},
		{"2 weeks", 1209600, true},
		{"", 0, false},
		{"abc", 0, false},
		{"10 foo", 0, false},
	}
	for _, c := range cases {
		got, ok := parseTimeSpan(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseTimeSpan(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestStartupRulesFireOnlyWhenCoupled: both rules key on something the unit
// declared (Notify=healthy, Restart=), so a container that asked for neither
// stays quiet. Warnings, so validate still passes.
func TestStartupRulesFireOnlyWhenCoupled(t *testing.T) {
	proj := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
gated: creidhne.#Quadlet & {name: "gated", units: #container: {
	Container: {Image: "docker.io/x", Notify: "healthy", HealthCmd: "CMD /bin/true"}
	Service: Restart: "on-failure"
}}
plain: creidhne.#Quadlet & {name: "plain", units: #container: Container: {Image: "docker.io/y"}}
norestart: creidhne.#Quadlet & {name: "norestart", units: #container: {
	Container: {Image: "docker.io/z"}
	Service: Restart: "no"
}}
kube: creidhne.#Quadlet & {name: "kube", units: #kube: {
	Kube: Yaml: ["app.yaml"]
	Service: Restart: "always"
}}
`)
	out, err := runCmd(t, "--dir", proj, "validate")
	if err != nil {
		t.Fatalf("both findings are warnings, validate must pass: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Notify=healthy gates start-up on the healthcheck but no TimeoutStartSec is set",
		"Restart=on-failure without RestartSec",
		// Restart= is not container-only: a .kube retries just as fast.
		"Restart=always without RestartSec",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing warning %q:\n%s", want, out)
		}
	}
	// Restart=no never relaunches, and a container with neither coupling has
	// nothing to warn about.
	for _, quiet := range []string{"plain.container", "norestart.container"} {
		if strings.Contains(out, quiet) {
			t.Fatalf("%s must not be flagged:\n%s", quiet, out)
		}
	}
}

// TestStartupRulesSatisfied: setting the fields silences both, including via
// the TimeoutSec shorthand, which covers start as well as stop.
func TestStartupRulesSatisfied(t *testing.T) {
	proj := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
explicit: creidhne.#Quadlet & {name: "explicit", units: #container: {
	Container: {Image: "docker.io/x", Notify: "healthy", HealthCmd: "CMD /bin/true"}
	Service: {Restart: "on-failure", TimeoutStartSec: "90s", RestartSec: "5s"}
}}
shorthand: creidhne.#Quadlet & {name: "shorthand", units: #container: {
	Container: {Image: "docker.io/y", Notify: "healthy", HealthCmd: "CMD /bin/true"}
	Service: {TimeoutSec: "120s"}
}}
`)
	out, err := runCmd(t, "--dir", proj, "validate")
	if err != nil {
		t.Fatalf("validate must pass: %v\n%s", err, out)
	}
	for _, unwanted := range []string{"service/start-timeout", "service/restart-delay"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("%s must be silenced:\n%s", unwanted, out)
		}
	}
}
