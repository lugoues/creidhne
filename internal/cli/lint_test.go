package cli

import (
	"strings"
	"testing"
)

func TestCmdLintRedundantResourceDep(t *testing.T) {
	dir := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
infra: creidhne.#Quadlet & {name: "infra", units: #network: {}}
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: {
		Container: {Image: "docker.io/app", Network: [infra.units.#network.#self]}
		Unit: After: [infra.units.#network.#service]
	}
}
`)
	out, err := runCmd(t, "--dir", dir, "lint")
	if err == nil {
		t.Fatalf("lint should exit non-zero when it finds issues:\n%s", out)
	}
	if !strings.Contains(out, "After= on infra.network is redundant with the Network=") {
		t.Fatalf("missing redundant-resource finding:\n%s", out)
	}
}

func TestCmdLintNetworkOnline(t *testing.T) {
	dir := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: {Container: {Image: "docker.io/app"}, Unit: {After: ["network-online.target"], Wants: ["network-online.target"]}}
}
`)
	out, err := runCmd(t, "--dir", dir, "lint")
	if err == nil {
		t.Fatalf("expected non-zero exit:\n%s", out)
	}
	if !strings.Contains(out, "After=network-online.target is redundant") ||
		!strings.Contains(out, "Wants=network-online.target is redundant") {
		t.Fatalf("missing network-online findings:\n%s", out)
	}
}

// A legitimate cross-container dependency (not a resource, not network-online)
// must not be flagged.
func TestCmdLintClean(t *testing.T) {
	dir := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
db: creidhne.#Quadlet & {name: "db", units: #container: Container: {Image: "docker.io/pg", ContainerName: "db"}}
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: {Container: {Image: "docker.io/app"}, Unit: After: [db.units.#container.#service]}
}
`)
	out, err := runCmd(t, "--dir", dir, "lint")
	if err != nil {
		t.Fatalf("clean project should exit zero, got %v:\n%s", err, out)
	}
	if !strings.Contains(out, "no redundant dependencies found") {
		t.Fatalf("expected clean message:\n%s", out)
	}
}

// With [Quadlet] DefaultDependencies=false, Quadlet omits the implicit
// network-online deps, so a hand-written one is required — must not be flagged.
func TestCmdLintNetworkOnlineRespectsDefaultDepsFalse(t *testing.T) {
	dir := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: {
		Container: {Image: "docker.io/app"}
		Quadlet: DefaultDependencies: false
		Unit: {After: ["network-online.target"], Wants: ["network-online.target"]}
	}
}
`)
	out, err := runCmd(t, "--dir", dir, "lint")
	if err != nil {
		t.Fatalf("network-online must not be flagged when DefaultDependencies=false; got %v:\n%s", err, out)
	}
	if !strings.Contains(out, "no redundant dependencies found") {
		t.Fatalf("expected clean:\n%s", out)
	}
}

// Two units generating the same systemd service must be rejected (distinct
// filenames slip past checkUniqueFilenames but the services collide). The check
// lives in eval.LoadAndValidate, so every loading command catches it — probe a
// few, including validate, which bypasses the CLI's loadQuadlets helper.
func TestServiceCollisionRejectedEverywhere(t *testing.T) {
	src := `package config
import "github.com/lugoues/creidhne@v0"
aaa: creidhne.#Quadlet & {name: "foo-network", units: #container: Container: {Image: "docker.io/x", ContainerName: "ord"}}
zzz: creidhne.#Quadlet & {name: "foo", units: #network: {}}
`
	for _, sub := range []string{"lint", "graph", "validate", "render", "plan"} {
		t.Run(sub, func(t *testing.T) {
			dir := setupProject(t, src)
			_, err := runCmd(t, "--dir", dir, sub)
			if err == nil || !strings.Contains(err.Error(), "duplicate systemd service") {
				t.Fatalf("%s: expected duplicate-service error, got: %v", sub, err)
			}
		})
	}
}

// BindsTo= on a referenced resource changes semantics (stronger than Quadlet's
// auto-dep) and may be intentional, so it must NOT be flagged.
func TestCmdLintIgnoresIntentionalBindsTo(t *testing.T) {
	dir := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
infra: creidhne.#Quadlet & {name: "infra", units: #network: {}}
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: {
		Container: {Image: "docker.io/app", Network: [infra.units.#network.#self]}
		Unit: BindsTo: [infra.units.#network.#service]
	}
}
`)
	out, err := runCmd(t, "--dir", dir, "lint")
	if err != nil {
		t.Fatalf("BindsTo= should not be flagged (exit zero), got %v:\n%s", err, out)
	}
	if strings.Contains(out, "redundant with") {
		t.Fatalf("BindsTo= was wrongly flagged as redundant:\n%s", out)
	}
}
