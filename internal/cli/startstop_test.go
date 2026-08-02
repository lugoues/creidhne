package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lugoues/creidhne/internal/systemd"
)

// stubStartStop swaps the start/stop enqueue vars, recording invocations.
func stubStartStop(t *testing.T) (started, stopped *[][]string) {
	t.Helper()
	var st, sp [][]string
	os, op := startAsyncFn, stopAsyncFn
	t.Cleanup(func() { startAsyncFn, stopAsyncFn = os, op })
	startAsyncFn = func(_ bool, units []string) error { st = append(st, units); return nil }
	stopAsyncFn = func(_ bool, units []string) error { sp = append(sp, units); return nil }
	return &st, &sp
}

// TestRunnableRows: start and stop target the runnable leaves; a pure-
// infrastructure quadlet falls back to all its services so acting on it still
// means something.
func TestRunnableRows(t *testing.T) {
	rows := []statusRow{
		{Quadlet: "app", Path: "app.container", Service: "app.service"},
		{Quadlet: "app", Path: "app-data.volume", Service: "app-data-volume.service"},
		{Quadlet: "app", Path: "app-img.build", Service: "app-img-build.service"},
		{Quadlet: "net", Path: "net-lan.network", Service: "net-lan-network.service"},
	}
	got := runnableRows(rows)
	var names []string
	for _, r := range got {
		names = append(names, r.Path)
	}
	want := "app.container,net-lan.network"
	if strings.Join(names, ",") != want {
		t.Fatalf("runnableRows = %v, want %s", names, want)
	}
}

// TestTrackedTransitionPreDone: pre-done units render checked with their note
// and are never enqueued; the others are.
func TestTrackedTransitionPreDone(t *testing.T) {
	forceRestartLive(t, false)
	started, _ := stubStartStop(t)
	stubRestart(t,
		func(bool, []string) error { t.Fatal("restart must not be enqueued"); return nil },
		func(_ bool, units []string) (map[string]string, error) { return map[string]string{}, nil },
		func(bool, []string) (map[string]systemd.UnitStatus, error) {
			return map[string]systemd.UnitStatus{"down.service": {ActiveState: "active", SubState: "running"}}, nil
		},
	)

	rows := rowsOf("up.service", "down.service")
	var buf bytes.Buffer
	err := trackedTransition(&buf, strings.NewReader(""), rows, false, false, startSpec,
		map[string]string{"up.service": "already running"})
	if err != nil {
		t.Fatal(err)
	}
	if len(*started) != 1 || strings.Join((*started)[0], ",") != "down.service" {
		t.Fatalf("only the down unit should be enqueued, got %v", *started)
	}
	out := buf.String()
	if !strings.Contains(out, "Starting 2 unit(s):") {
		t.Fatalf("verb header missing:\n%s", out)
	}
	if !strings.Contains(out, "already running") {
		t.Fatalf("pre-done note missing:\n%s", out)
	}
}

// TestStopFailurePredicate: a unit still active after its stop job clears is
// the failure; one left in systemd's failed state is down, which stop wanted.
func TestStopFailurePredicate(t *testing.T) {
	if stopSpec.failed(systemd.UnitStatus{ActiveState: "failed"}) {
		t.Fatal("failed state is stopped; not a stop failure")
	}
	if !stopSpec.failed(systemd.UnitStatus{ActiveState: "active"}) {
		t.Fatal("still-active after stop is the failure")
	}
	if !startSpec.failed(systemd.UnitStatus{ActiveState: "failed"}) {
		t.Fatal("failed state is a start failure")
	}
}

// TestCmdStartStopSelection: bare start/stop error; --all plus names errors.
func TestCmdStartStopSelection(t *testing.T) {
	proj, qd, _ := staleFixture(t)
	for _, verb := range []string{"start", "stop"} {
		if _, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, verb); err == nil {
			t.Fatalf("bare %s must error", verb)
		}
		if _, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, verb, "app", "--all"); err == nil {
			t.Fatalf("%s with names and --all must error", verb)
		}
	}
}

// TestCmdStartAlreadyRunning: everything up -> no enqueue, clear message. The
// stale fixture's app.service is running and its volume active (exited).
func TestCmdStartAlreadyRunning(t *testing.T) {
	proj, qd, _ := staleFixture(t)
	started, _ := stubStartStop(t)
	out, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, "start", "app")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "Nothing to start") {
		t.Fatalf("want idempotent message:\n%s", out)
	}
	if len(*started) != 0 {
		t.Fatalf("nothing should be enqueued, got %v", *started)
	}
}

// TestCmdStopStopsRunning: stop enqueues only the runnable leaf, never the
// volume oneshot — stopping infra propagates through quadlet's auto-wired
// Requires= to attachers in other quadlets; -y skips confirm.
func TestCmdStopStopsRunning(t *testing.T) {
	proj, qd, _ := staleFixture(t)
	_, stopped := stubStartStop(t)
	stubJobsClear(t)
	out, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, "stop", "app", "-y")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(*stopped) != 1 {
		t.Fatalf("stop should be enqueued once, got %v", *stopped)
	}
	units := strings.Join((*stopped)[0], ",")
	if !strings.Contains(units, "app.service") {
		t.Fatalf("app.service should be stopped, got %v", units)
	}
	if strings.Contains(units, "volume") {
		t.Fatalf("infra units must never be stopped (cross-quadlet cascade), got %v", units)
	}
	if !strings.Contains(out, "Stopping") {
		t.Fatalf("stop header missing:\n%s", out)
	}
}

// stubJobsClear makes the job queue empty (everything completes instantly) and
// the final status check report units stopped.
func stubJobsClear(t *testing.T) {
	t.Helper()
	op, os2, osl := pendingJobsFn, restartStatusFn, restartSleep
	t.Cleanup(func() { pendingJobsFn, restartStatusFn, restartSleep = op, os2, osl })
	pendingJobsFn = func(bool, []string) (map[string]string, error) { return map[string]string{}, nil }
	restartStatusFn = func(bool, []string) (map[string]systemd.UnitStatus, error) {
		return map[string]systemd.UnitStatus{}, nil
	}
	restartSleep = func(time.Duration) {}
}

// TestRestartPlainLeavesOnly: a plain named restart targets runnable leaves
// like start/stop — the volume oneshot stays out of the transaction (--stale
// keeps its own curated set, covered by TestRestartStale).
func TestRestartPlainLeavesOnly(t *testing.T) {
	proj, qd, recDir := staleFixture(t)
	out, err := runCmd(t, "--dir", proj, "--quadlet-dir", qd, "restart", "app", "-y")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	args, err := os.ReadFile(filepath.Join(recDir, "restart.args"))
	if err != nil {
		t.Fatalf("systemctl restart never invoked: %v", err)
	}
	if !strings.Contains(string(args), "app.service") || strings.Contains(string(args), "volume") {
		t.Fatalf("plain restart must target only the leaf, got %s", args)
	}
}
