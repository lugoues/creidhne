package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lugoues/creidhne/internal/state"
)

const staleV1 = `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {name: "app", units: {#container: Container: Image: "docker.io/x", volumes: data: {}}}
`

const staleV2 = `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {name: "app", units: {
	#container: Container: {Image: "docker.io/x", Environment: ["A=1"]}
	volumes: data: Volume: Label: ["l=1"]
}}
`

// reapply overwrites the project's main.cue and applies again.
func reapply(t *testing.T, proj, qd, mainCue string) {
	t.Helper()
	mustWrite(t, filepath.Join(proj, "main.cue"), mainCue)
	if out, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, "apply", "-y", "--reload-systemd=false"); err != nil {
		t.Fatalf("reapply: %v\n%s", err, out)
	}
}

// TestStateHistoryAcrossApplies: a content change moves the previous applied
// content into history, and InEffectAt selects by timestamp.
func TestStateHistoryAcrossApplies(t *testing.T) {
	proj, qd := applyProject(t, staleV1)
	s1, err := state.Load(qd)
	if err != nil {
		t.Fatal(err)
	}
	rec1, ok := s1.FileRecord("app.container")
	if !ok || rec1.Content == "" {
		t.Fatalf("first apply must record content: %+v", rec1)
	}
	if len(rec1.History) != 0 {
		t.Fatalf("fresh record must have no history: %+v", rec1.History)
	}

	reapply(t, proj, qd, staleV2)
	s2, err := state.Load(qd)
	if err != nil {
		t.Fatal(err)
	}
	rec2, _ := s2.FileRecord("app.container")
	if !strings.Contains(rec2.Content, "Environment=A=1") {
		t.Fatalf("current content not updated:\n%s", rec2.Content)
	}
	if len(rec2.History) != 1 || rec2.History[0].SHA256 != rec1.SHA256 || rec2.History[0].Content != rec1.Content {
		t.Fatalf("history must hold the superseded version: %+v", rec2.History)
	}
	if old, ok := rec2.InEffectAt(rec1.AppliedAt); !ok || old != rec1.Content {
		t.Fatalf("InEffectAt(first apply) must return the old content (ok=%v)", ok)
	}
	if cur, ok := rec2.InEffectAt(rec2.AppliedAt); !ok || cur != rec2.Content {
		t.Fatalf("InEffectAt(now) must return the current content (ok=%v)", ok)
	}

	// An unchanged re-apply carries content and history forward untouched.
	reapply(t, proj, qd, staleV2)
	s3, _ := state.Load(qd)
	rec3, _ := s3.FileRecord("app.container")
	if rec3.Content != rec2.Content || !reflect.DeepEqual(rec3.History, rec2.History) {
		t.Fatal("no-op apply must not disturb content or history")
	}
}

func TestChangedKeys(t *testing.T) {
	old := "[Container]\nImage=docker.io/x\nEnvironment=A=1\n"
	cur := "[Container]\nImage=docker.io/x\nEnvironment=A=2\nEnvironment=B=1\nReadOnly=true\n"
	got := changedKeys(old, cur)
	want := []string{"Environment", "ReadOnly"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changedKeys = %v, want %v", got, want)
	}
	if len(changedKeys(cur, cur)) != 0 {
		t.Fatal("identical content must yield no keys")
	}
}

// staleFixture applies v1 then v2, backdates the history so a fake runtime
// started between the two applies, and stubs systemctl accordingly.
// Returns the dir where the stub records restart invocations.
func staleFixture(t *testing.T) (proj, qd, recDir string) {
	t.Helper()
	proj, qd = applyProject(t, staleV1)
	reapply(t, proj, qd, staleV2)

	// Backdate the superseded versions: history entries an hour ago, current
	// AppliedAt stays "now", runtime started in between.
	s, err := state.Load(qd)
	if err != nil {
		t.Fatal(err)
	}
	q := s.Quadlets["app"]
	for i := range q.Files {
		for j := range q.Files[i].History {
			q.Files[i].History[j].AppliedAt = time.Now().Add(-time.Hour)
		}
	}
	s.Quadlets["app"] = q
	if err := state.Write(qd, s); err != nil {
		t.Fatal(err)
	}

	active := time.Now().Add(-30 * time.Minute).UTC().Format("Mon 2006-01-02 15:04:05 MST")
	show := "Id=app.service\nLoadState=loaded\nActiveState=active\nSubState=running\nNeedDaemonReload=no\nActiveEnterTimestamp=" + active + "\n\n" +
		"Id=app-data-volume.service\nLoadState=loaded\nActiveState=active\nSubState=exited\nNeedDaemonReload=no\nActiveEnterTimestamp=" + active + "\n"

	recDir = t.TempDir()
	bin := t.TempDir()
	systemctl := "#!/bin/sh\nif [ \"$1\" = \"restart\" ] || [ \"$2\" = \"restart\" ]; then\n  echo \"$@\" > " + recDir + "/restart.args\n  exit 0\nfi\ncat <<'EOF'\n" + show + "EOF\n"
	journalctl := "#!/bin/sh\necho \"$@\" > " + recDir + "/journalctl.args\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(systemctl), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "journalctl"), []byte(journalctl), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return proj, qd, recDir
}

// TestStaleAnnotations: status names the changed keys and flags kinds a
// restart cannot fix; diff --stale shows the content-level diff.
func TestStaleAnnotations(t *testing.T) {
	proj, qd, _ := staleFixture(t)
	out, err := statusOut(t, proj, qd)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"(stale: Environment)", "recreate required"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q:\n%s", want, out)
		}
	}

	dout, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, "diff", "--stale")
	if err != nil {
		t.Fatalf("%v\n%s", err, dout)
	}
	for _, want := range []string{"# app.container", "+ Environment=A=1", "restart will not apply", "2 stale unit(s)"} {
		if !strings.Contains(dout, want) {
			t.Fatalf("diff --stale missing %q:\n%s", want, dout)
		}
	}
}

// TestRestartStale: restarts the stale service, skips the stale volume with a
// warning, and passes the correct units to systemctl.
func TestRestartStale(t *testing.T) {
	proj, qd, recDir := staleFixture(t)
	out, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, "restart", "--stale", "-y")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "skipping app-data.volume") || !strings.Contains(out, "Restarted.") {
		t.Fatalf("restart output wrong:\n%s", out)
	}
	// Each line carries the staleness delta, status-style.
	if !strings.Contains(out, "(stale: Environment)") {
		t.Fatalf("restart listing missing the delta:\n%s", out)
	}
	args, err := os.ReadFile(filepath.Join(recDir, "restart.args"))
	if err != nil {
		t.Fatalf("systemctl restart never invoked: %v", err)
	}
	if !strings.Contains(string(args), "app.service") || strings.Contains(string(args), "app-data-volume.service") {
		t.Fatalf("restart args wrong: %s", args)
	}
}

// TestRestartRequiresSelection: bare restart without quadlets or --stale is an
// error, not an accidental fleet restart.
func TestRestartRequiresSelection(t *testing.T) {
	proj, qd, _ := staleFixture(t)
	if _, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, "restart"); err == nil {
		t.Fatal("restart with no selection must error")
	}
}

// TestLogsStale: journalctl gets every stale unit (volumes included; reading
// logs is harmless) without prompting.
func TestLogsStale(t *testing.T) {
	proj, qd, recDir := staleFixture(t)
	out, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, "logs", "--stale", "-n", "10")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	args, err := os.ReadFile(filepath.Join(recDir, "journalctl.args"))
	if err != nil {
		t.Fatalf("journalctl never invoked: %v", err)
	}
	for _, want := range []string{"-u app.service", "-u app-data-volume.service", "-n 10"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("journalctl args missing %q: %s", want, args)
		}
	}
}

const buildStaleV1 = `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {
	name: "app"
	units: {
		#build: {ContainerFile: "FROM alpine\n", Context: {"etc/app.conf": "x=1\n"}}
		#container: Container: Image: units.#build.#self
	}
}
`

const buildStaleV2 = `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {
	name: "app"
	units: {
		#build: {ContainerFile: "FROM alpine\n", Context: {"etc/app.conf": "x=2\n"}}
		#container: Container: Image: units.#build.#self
	}
}
`

func showBlock(svc, active string) string {
	return "Id=" + svc + "\nLoadState=loaded\nActiveState=active\nSubState=running\nNeedDaemonReload=no\nActiveEnterTimestamp=" + active + "\n"
}

// TestBuildStaleOnContextChange: changing a build's context moves the
// injected build-hash on both the .build unit and its consuming container,
// so both flag stale through the normal per-file mechanism — the build for
// "Containerfile/context", the container for "image rebuilt".
func TestBuildStaleOnContextChange(t *testing.T) {
	proj, qd := applyProject(t, buildStaleV1)
	reapply(t, proj, qd, buildStaleV2) // context x=1 -> x=2: hash moves on both units

	s, err := state.Load(qd)
	if err != nil {
		t.Fatal(err)
	}
	q := s.Quadlets["app"]
	var buildSvc, ctrSvc string
	for _, u := range q.Units {
		switch u.Kind {
		case "build":
			buildSvc = u.Service
		case "container":
			ctrSvc = u.Service
		}
	}
	if buildSvc == "" || ctrSvc == "" {
		t.Fatalf("services not recorded: build=%q container=%q", buildSvc, ctrSvc)
	}

	// Backdate the superseded (v1) versions to an hour ago so InEffectAt at
	// the runtime's ActiveEnter returns v1; current stays "now".
	for i := range q.Files {
		for j := range q.Files[i].History {
			q.Files[i].History[j].AppliedAt = time.Now().Add(-time.Hour)
		}
	}
	s.Quadlets["app"] = q
	if err := state.Write(qd, s); err != nil {
		t.Fatal(err)
	}

	active := time.Now().Add(-30 * time.Minute).UTC().Format("Mon 2006-01-02 15:04:05 MST")
	fakeSystemctl(t, showBlock(buildSvc, active)+"\n"+showBlock(ctrSvc, active))

	out, err := statusOut(t, proj, qd)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "app.build") || !strings.Contains(out, "Containerfile/context") {
		t.Fatalf("build must be stale with a Containerfile/context note:\n%s", out)
	}
	if !strings.Contains(out, "app.container") || !strings.Contains(out, "image rebuilt") {
		t.Fatalf("consumer must be stale with an image-rebuilt note:\n%s", out)
	}
}
