package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lugoues/creidhne/internal/reconcile"
	"github.com/lugoues/creidhne/internal/state"
)

const stateMain = `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {name: "app", units: #container: Container: Image: "docker.io/x"}
`

// applyProject runs apply -y into a fresh quadlet dir and returns both dirs.
func applyProject(t *testing.T, mainCue string) (projDir, quadletDir string) {
	t.Helper()
	projDir = setupProject(t, mainCue)
	quadletDir = t.TempDir()
	if out, err := runCmd(t, "--dir", projDir, "--quadlet-dir", quadletDir, "apply", "-y", "--reload-systemd=false"); err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	return projDir, quadletDir
}

// TestApplyRecordsState: apply writes crei.state with the manifest and hashes
// matching the bytes on disk.
func TestApplyRecordsState(t *testing.T) {
	_, qd := applyProject(t, stateMain)
	s, err := state.Load(qd)
	if err != nil || s == nil {
		t.Fatalf("state after apply: %v, %v", s, err)
	}
	rec, ok := s.Quadlets["app"]
	if !ok || len(rec.Units) != 1 || rec.Units[0].Service != "app.service" {
		t.Fatalf("state manifest wrong: %+v", rec)
	}
	if len(rec.Files) != 1 || rec.Files[0].Path != "app.container" {
		t.Fatalf("state files wrong: %+v", rec.Files)
	}
	disk, err := os.ReadFile(filepath.Join(qd, "app.container"))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Files[0].SHA256 != state.HashBytes(disk) {
		t.Fatal("recorded hash does not match the bytes on disk")
	}
}

// TestNoopApplyAdoptsState: an apply with nothing to do still (re)creates the
// state file, adopting pre-state-file deployments.
func TestNoopApplyAdoptsState(t *testing.T) {
	proj, qd := applyProject(t, stateMain)
	if err := os.Remove(state.Path(qd)); err != nil {
		t.Fatal(err)
	}
	out, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, "apply", "-y", "--reload-systemd=false")
	if err != nil {
		t.Fatalf("no-op apply: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Nothing to do.") {
		t.Fatalf("expected a no-op apply:\n%s", out)
	}
	if s, err := state.Load(qd); err != nil || s == nil {
		t.Fatalf("no-op apply should adopt state: %v, %v", s, err)
	}
}

// TestApplyCarriesForwardAppliedAt: re-applying with one file changed updates
// only that file's AppliedAt; the untouched file keeps its original.
func TestApplyCarriesForwardAppliedAt(t *testing.T) {
	twoUnits := `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {name: "app", units: #container: Container: Image: "docker.io/x"}
db: creidhne.#Quadlet & {name: "db", units: #container: Container: {Image: "docker.io/pg", ContainerName: "db"}}
`
	proj, qd := applyProject(t, twoUnits)
	first, err := state.Load(qd)
	if err != nil {
		t.Fatal(err)
	}
	// Change only db, re-apply.
	changed := strings.Replace(twoUnits, "docker.io/pg", "docker.io/pg:16", 1)
	if err := os.WriteFile(filepath.Join(proj, "main.cue"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, "apply", "-y", "--reload-systemd=false"); err != nil {
		t.Fatalf("second apply: %v\n%s", err, out)
	}
	second, err := state.Load(qd)
	if err != nil {
		t.Fatal(err)
	}
	appBefore, _ := first.FileRecord("app.container")
	appAfter, _ := second.FileRecord("app.container")
	if !appAfter.AppliedAt.Equal(appBefore.AppliedAt) {
		t.Fatalf("untouched app.container AppliedAt changed: %v -> %v", appBefore.AppliedAt, appAfter.AppliedAt)
	}
	dbBefore, _ := first.FileRecord("db.container")
	dbAfter, _ := second.FileRecord("db.container")
	if !dbAfter.AppliedAt.After(dbBefore.AppliedAt) {
		t.Fatalf("changed db.container AppliedAt not advanced: %v -> %v", dbBefore.AppliedAt, dbAfter.AppliedAt)
	}
}

// TestPlanIgnoresStateFile: crei.state in the quadlet dir is invisible to the
// reconcile scan, so plan never schedules it for removal.
func TestPlanIgnoresStateFile(t *testing.T) {
	proj, qd := applyProject(t, stateMain)
	existing, err := reconcile.ListExisting(qd)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range existing {
		if strings.Contains(p, state.Filename) {
			t.Fatalf("ListExisting must not see %s: %v", state.Filename, existing)
		}
	}
	out, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, "plan")
	if err != nil {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	if strings.Contains(out, state.Filename) {
		t.Fatalf("plan output mentions the state file:\n%s", out)
	}
}
