package cli

import (
	"strings"
	"testing"
)

// pairProject builds a pair-marked network in svc with n cross-quadlet
// attachers beyond the service itself.
func pairProject(extraAttachers int) string {
	s := `package config
import "github.com/lugoues/creidhne@v0"
svc: creidhne.#Quadlet & {
	name: "svc"
	units: {
		networks: proxy: Network: Label: ["creidhne.pair=svc"]
		#container: Container: {Image: "docker.io/x", Network: [units.networks.proxy.#self]}
	}
}
`
	names := []string{"proxy1", "proxy2", "proxy3"}
	for i := 0; i < extraAttachers; i++ {
		s += `
` + names[i] + `: creidhne.#Quadlet & {
	name: "` + names[i] + `"
	units: #container: Container: {Image: "docker.io/p", Network: [svc.units.networks.proxy.#self]}
}
`
	}
	return s
}

// TestPairCardinalityExact: service + one proxy is the contract; no findings.
func TestPairCardinalityExact(t *testing.T) {
	proj := setupProject(t, pairProject(1))
	out, err := runCmd(t, "--dir", proj, "validate")
	if err != nil {
		t.Fatalf("exactly two attachers must validate: %v\n%s", err, out)
	}
	if strings.Contains(out, "pair network") {
		t.Fatalf("no pair finding expected:\n%s", out)
	}
}

// TestPairCardinalityOver: a third attacher breaks the isolation contract and
// fails validate; lint reports it too.
func TestPairCardinalityOver(t *testing.T) {
	proj := setupProject(t, pairProject(2))
	out, err := runCmd(t, "--dir", proj, "validate")
	if err == nil {
		t.Fatalf("three attachers must fail validate:\n%s", out)
	}
	for _, want := range []string{"svc-proxy.network", "3 attachers", "the contract is exactly two"} {
		if !strings.Contains(out, want) {
			t.Fatalf("validate output missing %q:\n%s", want, out)
		}
	}
	if _, err := runCmd(t, "--dir", proj, "lint"); err == nil {
		t.Fatal("lint must exit non-zero on findings")
	}
}

// TestPairCardinalityUnder: only the service attached is a warning (wiring
// incomplete), not an error: validate passes but reports.
func TestPairCardinalityUnder(t *testing.T) {
	proj := setupProject(t, pairProject(0))
	out, err := runCmd(t, "--dir", proj, "validate")
	if err != nil {
		t.Fatalf("one attacher is a warning, validate must pass: %v\n%s", err, out)
	}
	if !strings.Contains(out, "wiring incomplete") {
		t.Fatalf("expected incomplete-wiring warning:\n%s", out)
	}
}

// TestDuplicateRuntimeName: the same effective ContainerName on two quadlets'
// containers is an error (podman uniqueness, name-based references).
func TestDuplicateRuntimeName(t *testing.T) {
	proj := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
a: creidhne.#Quadlet & {name: "a", units: #container: Container: {Image: "docker.io/x", ContainerName: "db"}}
b: creidhne.#Quadlet & {name: "b", units: #container: Container: {Image: "docker.io/y", ContainerName: "db"}}
`)
	out, err := runCmd(t, "--dir", proj, "validate")
	if err == nil {
		t.Fatalf("duplicate runtime name must fail validate:\n%s", out)
	}
	for _, want := range []string{`"db" is shared by`, "a.container", "b.container"} {
		if !strings.Contains(out, want) {
			t.Fatalf("validate output missing %q:\n%s", want, out)
		}
	}
}

// TestOrphanAndRouterWarnings: an unattached plain network and a router name
// defined by two units both warn; validate still passes.
func TestOrphanAndRouterWarnings(t *testing.T) {
	proj := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {
	name: "app"
	units: {
		networks: unused: {}
		#container: Container: {Image: "docker.io/x", Label: ["traefik.http.routers.web.rule=Host(x)"]}
		containers: side: Container: {Image: "docker.io/y", Label: ["traefik.http.routers.web.priority=9"]}
	}
}
`)
	out, err := runCmd(t, "--dir", proj, "validate")
	if err != nil {
		t.Fatalf("warnings only, validate must pass: %v\n%s", err, out)
	}
	for _, want := range []string{"app-unused.network", "attaches this network", `router "web" is defined by`} {
		if !strings.Contains(out, want) {
			t.Fatalf("validate output missing %q:\n%s", want, out)
		}
	}
	// Same findings through lint, which exits non-zero on any finding.
	lout, lerr := runCmd(t, "--dir", proj, "lint")
	if lerr == nil {
		t.Fatalf("lint must exit non-zero:\n%s", lout)
	}
	if !strings.Contains(lout, "finding(s)") {
		t.Fatalf("lint summary missing:\n%s", lout)
	}
}

// TestLintFocusFiltersRuleFindings: naming a quadlet hides findings attributed
// to other quadlets' units.
func TestLintFocusFiltersRuleFindings(t *testing.T) {
	proj := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {
	name: "app"
	units: {networks: unused: {}, #container: Container: Image: "docker.io/x"}
}
other: creidhne.#Quadlet & {name: "other", units: #container: Container: Image: "docker.io/y"}
`)
	out, _ := runCmd(t, "--dir", proj, "lint", "other")
	if strings.Contains(out, "app-unused.network") {
		t.Fatalf("focus 'other' must hide app's findings:\n%s", out)
	}
}
