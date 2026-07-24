package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/lugoues/creidhne/internal/systemd"
)

// stubRestart swaps the systemd boundary trackedRestart calls, restoring the
// real functions after the test. restartSleep is neutralized so the poll loop
// runs at full speed.
func stubRestart(t *testing.T,
	async func(bool, []string) error,
	pending func(bool, []string) (map[string]string, error),
	status func(bool, []string) (map[string]systemd.UnitStatus, error),
) {
	t.Helper()
	oa, op, os, osl := restartAsyncFn, pendingJobsFn, restartStatusFn, restartSleep
	t.Cleanup(func() { restartAsyncFn, pendingJobsFn, restartStatusFn, restartSleep = oa, op, os, osl })
	restartAsyncFn, pendingJobsFn, restartStatusFn = async, pending, status
	restartSleep = func(time.Duration) {}
}

// TestTrackedRestartStraggler: the checklist fills in over successive polls —
// the fast unit clears on the first, the slow one only after it drops from the
// job queue — and the run ends with the completion line.
func TestTrackedRestartStraggler(t *testing.T) {
	enqueued := false
	poll := 0
	stubRestart(t,
		func(bool, []string) error { enqueued = true; return nil },
		func(bool, []string) (map[string]string, error) {
			poll++
			if poll >= 3 {
				return map[string]string{}, nil // slow.service finally done
			}
			return map[string]string{"slow.service": "running"}, nil // fast already gone
		},
		func(bool, []string) (map[string]systemd.UnitStatus, error) {
			return map[string]systemd.UnitStatus{
				"fast.service": {ActiveState: "active", SubState: "running"},
				"slow.service": {ActiveState: "active", SubState: "running"},
			}, nil
		},
	)

	var buf bytes.Buffer
	if err := trackedRestart(&buf, false, []string{"fast.service", "slow.service"}); err != nil {
		t.Fatal(err)
	}
	if !enqueued {
		t.Fatal("restart was never enqueued")
	}
	out := buf.String()
	for _, want := range []string{"fast.service", "slow.service", "Restarted."} {
		if !strings.Contains(out, want) {
			t.Fatalf("checklist missing %q:\n%s", want, out)
		}
	}
	// fast must be checked off before slow (it cleared first).
	if strings.Index(out, "fast.service") > strings.Index(out, "slow.service") {
		t.Fatalf("fast should complete before slow:\n%s", out)
	}
}

// TestWaitingLabel: the pending list is shown in input order and capped so a
// wide fan-out stays one line.
func TestWaitingLabel(t *testing.T) {
	units := []string{"a.service", "b.service", "c.service", "d.service", "e.service"}
	rem := map[string]bool{"b.service": true, "d.service": true, "e.service": true, "a.service": true}
	got := waitingLabel(units, rem)
	if got != "a.service, b.service, d.service +1 more" {
		t.Fatalf("waitingLabel = %q", got)
	}
	if s := waitingLabel(units, map[string]bool{"c.service": true}); s != "c.service" {
		t.Fatalf("single pending = %q", s)
	}
}

// TestRestartSpinnerRenders: an active spinner draws an animated, in-place line
// (carriage-return + clear, a frame, the label) and clear erases it; an inert
// one writes nothing.
func TestRestartSpinnerRenders(t *testing.T) {
	old := restartSleep
	defer func() { restartSleep = old }()
	restartSleep = func(time.Duration) {}

	var buf bytes.Buffer
	s := &restartSpinner{w: &buf, active: true}
	s.wait(250*time.Millisecond, "slow.service") // spans a few frames
	out := buf.String()
	if !strings.Contains(out, "\r\033[K") || !strings.Contains(out, "slow.service") {
		t.Fatalf("spinner did not render an in-place labelled line:\n%q", out)
	}
	if !strings.Contains(out, spinFrames[0]) {
		t.Fatalf("spinner drew no frame glyph:\n%q", out)
	}
	buf.Reset()
	s.clear()
	if buf.String() != "\r\033[K" {
		t.Fatalf("clear = %q, want the erase sequence", buf.String())
	}

	// Inert spinner (non-terminal) writes nothing.
	var inert bytes.Buffer
	is := &restartSpinner{w: &inert, active: false}
	is.wait(time.Second, "x")
	is.clear()
	if inert.Len() != 0 {
		t.Fatalf("inert spinner wrote %q", inert.String())
	}
}

// TestTrackedRestartReportsFailed: a unit whose job clears but ends in the
// failed state is reported and turns the command non-zero.
func TestTrackedRestartReportsFailed(t *testing.T) {
	stubRestart(t,
		func(bool, []string) error { return nil },
		func(bool, []string) (map[string]string, error) { return map[string]string{}, nil },
		func(bool, []string) (map[string]systemd.UnitStatus, error) {
			return map[string]systemd.UnitStatus{"bad.service": {ActiveState: "failed"}}, nil
		},
	)

	var buf bytes.Buffer
	err := trackedRestart(&buf, false, []string{"bad.service"})
	if err == nil {
		t.Fatal("a unit that came back failed must return an error")
	}
	if !strings.Contains(buf.String(), "bad.service failed") {
		t.Fatalf("failure not reported:\n%s", buf.String())
	}
}
