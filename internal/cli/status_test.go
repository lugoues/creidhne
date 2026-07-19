package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// statusOut runs `crei status` for a project/quadlet-dir pair.
func statusOut(t *testing.T, proj, qd string, extra ...string) (string, error) {
	t.Helper()
	args := append([]string{"--dir", proj, "--quadlet-dir", qd, "status"}, extra...)
	return runCmd(t, args...)
}

func TestStatusFreshAllMissing(t *testing.T) {
	proj := setupProject(t, stateMain)
	qd := t.TempDir()
	out, err := statusOut(t, proj, qd)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "missing") {
		t.Fatalf("fresh project should show missing units:\n%s", out)
	}
	if _, err := statusOut(t, proj, qd, "--check"); err == nil {
		t.Fatal("--check must fail on missing units")
	}
}

func TestStatusSyncedAfterApply(t *testing.T) {
	proj, qd := applyProject(t, stateMain)
	out, err := statusOut(t, proj, qd)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "synced") {
		t.Fatalf("applied project should be synced:\n%s", out)
	}
	// Runtime is unavailable in the test environment; that alone must not
	// fail --check (environment problem, not convergence).
	if out, err := statusOut(t, proj, qd, "--check"); err != nil {
		t.Fatalf("--check should pass when synced (runtime unavailable): %v\n%s", err, out)
	}
}

func TestStatusMatrix(t *testing.T) {
	twoUnits := `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {name: "app", units: {#container: Container: Image: "docker.io/x", #volume: {}}}
`
	proj, qd := applyProject(t, twoUnits)

	// pending: edit the CUE; orphan: drop the volume from the CUE.
	next := `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {name: "app", units: #container: Container: Image: "docker.io/x:2"}
`
	if err := os.WriteFile(filepath.Join(proj, "main.cue"), []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
	// tampered: hand-edit a deployed file. (app.container is also pending;
	// tamper evidence must win.)
	f := filepath.Join(qd, "app.container")
	raw, _ := os.ReadFile(f)
	if err := os.WriteFile(f, append(raw, []byte("# hand edit\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	// foreign: a file crei never recorded.
	if err := os.WriteFile(filepath.Join(qd, "random.container"), []byte("[Container]\nImage=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := statusOut(t, proj, qd)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"tampered", "orphan", "foreign", "(unmanaged)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("matrix missing %q:\n%s", want, out)
		}
	}
	if _, err := statusOut(t, proj, qd, "--check"); err == nil {
		t.Fatal("--check must fail on tampered/orphan/foreign")
	}
}

func TestStatusBrokenEvalDegrades(t *testing.T) {
	proj, qd := applyProject(t, stateMain)
	if err := os.WriteFile(filepath.Join(proj, "main.cue"), []byte("package config\napp: 3 & \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := statusOut(t, proj, qd)
	if err != nil {
		t.Fatalf("status must not error on broken eval: %v\n%s", err, out)
	}
	if !strings.Contains(out, "eval failed") || !strings.Contains(out, "applied") {
		t.Fatalf("broken eval should degrade to recorded state with a note:\n%s", out)
	}
	if _, err := statusOut(t, proj, qd, "--check"); err == nil {
		t.Fatal("--check must fail while eval is broken")
	}
}

// fakeSystemctl puts a systemctl stub on PATH that emits the given show
// output, so the runtime columns can be exercised without systemd.
func fakeSystemctl(t *testing.T, showOutput string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncat <<'EOF'\n" + showOutput + "EOF\n"
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestStatusRuntimeColumns(t *testing.T) {
	twoQuads := `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {name: "app", units: {#container: Container: Image: "docker.io/x", networks: net: {}}}
db: creidhne.#Quadlet & {name: "db", units: #container: Container: {Image: "docker.io/pg", ContainerName: "db"}}
`
	proj, qd := applyProject(t, twoQuads)
	// app: running since long before the apply above -> running (stale), and
	// needing a daemon reload. db: failed.
	fakeSystemctl(t, `Id=app.service
LoadState=loaded
ActiveState=active
SubState=running
NeedDaemonReload=yes
ActiveEnterTimestamp=Fri 2020-01-03 10:00:00 UTC

Id=db.service
LoadState=loaded
ActiveState=failed
SubState=failed
NeedDaemonReload=no
ActiveEnterTimestamp=

Id=app-net-network.service
LoadState=loaded
ActiveState=active
SubState=exited
NeedDaemonReload=no
ActiveEnterTimestamp=
`)
	out, err := statusOut(t, proj, qd)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// The state glyph leads each row systemctl-style; oneshot units
	// (network) read active, as healthy as running.
	for _, want := range []string{"● app.container", "✗ db.container", "● app-net.network", "active", "(stale)", "reload needed", "failed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("runtime columns missing %q:\n%s", want, out)
		}
	}
	if _, err := statusOut(t, proj, qd, "--check"); err == nil {
		t.Fatal("--check must fail on failed/stale/reload-needed units")
	}
}

const buildQuad = `package config
import "github.com/lugoues/creidhne@v0"
hermes: creidhne.#Quadlet & {
	name: "hermes"
	units: {
		#build: {ContainerFile: "FROM alpine\n", Context: {"etc/app.conf": "x=1\n"}}
		#container: Container: Image: units.#build.#self
	}
}
traefik: creidhne.#Quadlet & {name: "traefik", units: #container: Container: Image: "docker.io/traefik"}
`

// TestStatusFilterByQuadlet: naming quadlets narrows the view, unknown names
// error with the known set, and the filter still resolves against recorded
// state when the eval is broken.
func TestStatusFilterByQuadlet(t *testing.T) {
	proj, qd := applyProject(t, buildQuad)

	out, err := statusOut(t, proj, qd, "hermes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "traefik") {
		t.Fatalf("filtered view leaked another quadlet:\n%s", out)
	}
	if !strings.Contains(out, "hermes.container") {
		t.Fatalf("filtered view missing hermes rows:\n%s", out)
	}

	if _, err := statusOut(t, proj, qd, "nope"); err == nil || !strings.Contains(err.Error(), `unknown quadlet "nope"`) {
		t.Fatalf("unknown quadlet should error with the known set, got: %v", err)
	}

	// Broken eval: the name must still resolve, via crei.state.
	if err := os.WriteFile(filepath.Join(proj, "main.cue"), []byte("package config\nx: 1 & 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = statusOut(t, proj, qd, "hermes")
	if err != nil {
		t.Fatalf("filter must work from recorded state under broken eval: %v\n%s", err, out)
	}
	if !strings.Contains(out, "hermes.container") || strings.Contains(out, "traefik") {
		t.Fatalf("recorded-state filter wrong:\n%s", out)
	}
}

// TestStatusGroupedByQuadlet: the table groups units under a quadlet-name
// header line at column zero, unit rows indented beneath it, no QUADLET
// column.
func TestStatusGroupedByQuadlet(t *testing.T) {
	proj, qd := applyProject(t, buildQuad)
	out, err := statusOut(t, proj, qd)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"\nhermes\n  hermes.build", "\ntraefik\n  traefik.container"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing group header + indented row %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "QUADLET") {
		t.Fatalf("grouped view should not have a QUADLET column:\n%s", out)
	}
}

// TestStatusArtifactCollapse: the unfiltered overview hides synced images/
// rows (with a count note) but keeps non-synced ones; a named quadlet shows
// everything.
func TestStatusArtifactCollapse(t *testing.T) {
	proj, qd := applyProject(t, buildQuad)

	out, err := statusOut(t, proj, qd)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "images/hermes") {
		t.Fatalf("unfiltered view should hide synced images/ rows:\n%s", out)
	}
	if !strings.Contains(out, "synced images/ hidden") {
		t.Fatalf("missing hidden-count note:\n%s", out)
	}

	out, err = statusOut(t, proj, qd, "hermes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "images/hermes.Containerfile") || !strings.Contains(out, "images/hermes.context/etc/app.conf") {
		t.Fatalf("filtered view should show artifact rows:\n%s", out)
	}

	// A tampered artifact must surface even in the unfiltered overview.
	f := filepath.Join(qd, "images", "hermes.context", "etc", "app.conf")
	if err := os.WriteFile(f, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = statusOut(t, proj, qd)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "images/hermes.context/etc/app.conf") || !strings.Contains(out, "tampered") {
		t.Fatalf("tampered artifact hidden from overview:\n%s", out)
	}
}

// TestStatusProblemsFilter: --problems shows only rows needing attention;
// inactive is not a problem; all-clean prints "No problems."
func TestStatusProblemsFilter(t *testing.T) {
	threeQuads := `package config
import "github.com/lugoues/creidhne@v0"
web: creidhne.#Quadlet & {name: "web", units: #container: Container: Image: "docker.io/nginx"}
db: creidhne.#Quadlet & {name: "db", units: #container: Container: {Image: "docker.io/pg", ContainerName: "db"}}
idle: creidhne.#Quadlet & {name: "idle", units: #container: Container: {Image: "docker.io/idle", ContainerName: "idle"}}
`
	proj, qd := applyProject(t, threeQuads)
	fakeSystemctl(t, `Id=web.service
LoadState=loaded
ActiveState=active
SubState=running
NeedDaemonReload=no
ActiveEnterTimestamp=Fri 2036-01-01 10:00:00 UTC

Id=db.service
LoadState=loaded
ActiveState=failed
SubState=failed
NeedDaemonReload=no
ActiveEnterTimestamp=

Id=idle.service
LoadState=loaded
ActiveState=inactive
SubState=dead
NeedDaemonReload=no
ActiveEnterTimestamp=
`)
	out, err := statusOut(t, proj, qd, "--problems")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// web is running (future ActiveEnter, so not stale) and idle is inactive:
	// neither is a problem. db failed: shown.
	if !strings.Contains(out, "db.container") {
		t.Fatalf("--problems must show the failed unit:\n%s", out)
	}
	if strings.Contains(out, "web.container") || strings.Contains(out, "idle.container") {
		t.Fatalf("--problems leaked healthy/inactive rows:\n%s", out)
	}
	// All clean: running + inactive only.
	fakeSystemctl(t, `Id=web.service
LoadState=loaded
ActiveState=active
SubState=running
NeedDaemonReload=no
ActiveEnterTimestamp=Fri 2036-01-01 10:00:00 UTC

Id=db.service
LoadState=loaded
ActiveState=inactive
SubState=dead
NeedDaemonReload=no
ActiveEnterTimestamp=

Id=idle.service
LoadState=loaded
ActiveState=inactive
SubState=dead
NeedDaemonReload=no
ActiveEnterTimestamp=
`)
	out, err = statusOut(t, proj, qd, "--problems")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "No problems.") {
		t.Fatalf("clean deployment should print No problems:\n%s", out)
	}
}

// TestStatusJSON: --format json emits every row (artifacts included, nothing
// hidden) with stable keys and the clean verdict.
func TestStatusJSON(t *testing.T) {
	proj, qd := applyProject(t, buildQuad)
	out, err := statusOut(t, proj, qd, "--format", "json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var doc struct {
		Scope string   `json:"scope"`
		Notes []string `json:"notes"`
		Clean bool     `json:"clean"`
		Rows  []struct {
			Quadlet  string `json:"quadlet"`
			Path     string `json:"path"`
			Artifact bool   `json:"artifact"`
			Disk     string `json:"disk"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json did not parse: %v\n%s", err, out)
	}
	var artifacts, units int
	for _, r := range doc.Rows {
		if r.Disk != "synced" {
			t.Fatalf("expected all synced, got %+v", r)
		}
		if r.Artifact {
			artifacts++
		} else {
			units++
		}
	}
	if artifacts == 0 || units == 0 {
		t.Fatalf("json must include artifact and unit rows: %+v", doc.Rows)
	}
	// Runtime is unavailable here, which is an environment note, not unclean.
	if !doc.Clean {
		t.Fatalf("synced deployment should be clean: %+v", doc)
	}

	if _, err := statusOut(t, proj, qd, "--format", "svg"); err == nil {
		t.Fatal("invalid --format must error")
	}
}

func TestStatusEmptyEverything(t *testing.T) {
	proj := setupProject(t, stateMain)
	// Point at an empty quadlet dir but break eval, no state: minimal view.
	if err := os.WriteFile(filepath.Join(proj, "main.cue"), []byte("package config\nx: 1 & 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := statusOut(t, proj, t.TempDir())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "No units") {
		t.Fatalf("expected the empty message:\n%s", out)
	}
}
