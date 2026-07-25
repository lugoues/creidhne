package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lugoues/creidhne/internal/systemd"
)

// stubRestart swaps the systemd boundary trackedRestart calls, restoring the
// real functions after. restartSleep is neutralized so the loop runs at full
// speed; forceLive optionally pins the terminal/plain branch.
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

// forceRestartLive pins whether trackedRestart takes the in-place terminal path.
func forceRestartLive(t *testing.T, live bool) {
	t.Helper()
	old := restartLive
	t.Cleanup(func() { restartLive = old })
	restartLive = func(io.Writer) bool { return live }
}

func rowsOf(names ...string) []statusRow {
	rows := make([]statusRow, len(names))
	for i, n := range names {
		rows[i] = statusRow{Service: n}
	}
	return rows
}

// TestPlainRestartStraggler (pipe/CI path): each unit prints once as its job
// clears, fast before slow, and there is no summary footer.
func TestPlainRestartStraggler(t *testing.T) {
	forceRestartLive(t, false)
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
	if err := trackedRestart(&buf, strings.NewReader(""), rowsOf("fast.service", "slow.service"), false, false); err != nil {
		t.Fatal(err)
	}
	if !enqueued {
		t.Fatal("restart was never enqueued")
	}
	out := buf.String()
	for _, want := range []string{"Restarting 2 unit(s):", "fast.service", "slow.service"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Restarted.") {
		t.Fatalf("the summary footer should be gone:\n%s", out)
	}
	if strings.Index(out, "fast.service") > strings.Index(out, "slow.service") {
		t.Fatalf("fast should complete before slow:\n%s", out)
	}
}

// TestLiveRestartRedraws (terminal path): the block is drawn with a spinner and
// redrawn in place (cursor-up + line-clear) until each unit shows a check.
func TestLiveRestartRedraws(t *testing.T) {
	forceRestartLive(t, true)
	poll := 0
	stubRestart(t,
		func(bool, []string) error { return nil },
		func(bool, []string) (map[string]string, error) {
			poll++
			if poll >= 2 {
				return map[string]string{}, nil
			}
			return map[string]string{"b.service": "running"}, nil
		},
		func(bool, []string) (map[string]systemd.UnitStatus, error) {
			return map[string]systemd.UnitStatus{
				"a.service": {ActiveState: "active"},
				"b.service": {ActiveState: "active"},
			}, nil
		},
	)

	var buf bytes.Buffer
	if err := trackedRestart(&buf, strings.NewReader(""), rowsOf("a.service", "b.service"), false, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "\033[2A") { // cursor up 2 (the block height)
		t.Fatalf("block was not redrawn in place:\n%q", out)
	}
	if !strings.Contains(out, "\r\033[K") { // per-line clear
		t.Fatalf("lines were not cleared before redraw:\n%q", out)
	}
	if !strings.Contains(out, spinFrames[0]) { // a spinner frame appeared
		t.Fatalf("no spinner frame rendered:\n%q", out)
	}
	if !strings.Contains(out, "✓") { // units settle to checks
		t.Fatalf("units never checked off:\n%q", out)
	}
}

// TestLiveRestartConfirmInPlace: with a confirm, the block is printed once
// (pending dots), the prompt sits below it, and on "yes" the prompt is erased
// (save/restore + clear-to-end) and the same lines animate — one list, not two.
func TestLiveRestartConfirmInPlace(t *testing.T) {
	forceRestartLive(t, true)
	enqueued := false
	stubRestart(t,
		func(bool, []string) error { enqueued = true; return nil },
		func(bool, []string) (map[string]string, error) { return map[string]string{}, nil },
		func(bool, []string) (map[string]systemd.UnitStatus, error) {
			return map[string]systemd.UnitStatus{"a.service": {ActiveState: "active"}}, nil
		},
	)

	var buf bytes.Buffer
	if err := trackedRestart(&buf, strings.NewReader("y\n"), rowsOf("a.service"), false, true); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !enqueued {
		t.Fatal("a yes answer must enqueue the restart")
	}
	for _, want := range []string{
		"Restart? [y/N]", // prompt shown below the block
		"\033[s",         // cursor saved before the prompt
		"\033[u\033[J",   // restored and cleared after (prompt erased)
		"\033[1A",        // block animated in place (single unit -> up 1)
		"✓",              // settled to a check
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in live-confirm output:\n%q", want, out)
		}
	}
	// The unit is listed once as a block line, not a second time as a preview.
	if strings.Contains(out, "will restart:") {
		t.Fatalf("live path must not print the plain preview list:\n%q", out)
	}
}

// TestLiveRestartConfirmAborts: "no" aborts without enqueuing.
func TestLiveRestartConfirmAborts(t *testing.T) {
	forceRestartLive(t, true)
	enqueued := false
	stubRestart(t,
		func(bool, []string) error { enqueued = true; return nil },
		func(bool, []string) (map[string]string, error) { return map[string]string{}, nil },
		func(bool, []string) (map[string]systemd.UnitStatus, error) { return nil, nil },
	)
	var buf bytes.Buffer
	if err := trackedRestart(&buf, strings.NewReader("n\n"), rowsOf("a.service"), false, true); err != nil {
		t.Fatal(err)
	}
	if enqueued {
		t.Fatal("a no answer must not enqueue")
	}
	if !strings.Contains(buf.String(), "Aborted.") {
		t.Fatalf("expected Aborted.:\n%q", buf.String())
	}
}

// TestRestartReportsFailed: a unit whose job clears but ends failed is marked
// with a cross and turns the command non-zero. (plain path for a clean buffer)
func TestRestartReportsFailed(t *testing.T) {
	forceRestartLive(t, false)
	stubRestart(t,
		func(bool, []string) error { return nil },
		func(bool, []string) (map[string]string, error) { return map[string]string{}, nil },
		func(bool, []string) (map[string]systemd.UnitStatus, error) {
			return map[string]systemd.UnitStatus{"bad.service": {ActiveState: "failed"}}, nil
		},
	)

	var buf bytes.Buffer
	err := trackedRestart(&buf, strings.NewReader(""), rowsOf("bad.service"), false, false)
	if err == nil {
		t.Fatal("a unit that came back failed must return an error")
	}
	if !strings.Contains(err.Error(), "bad.service") {
		t.Fatalf("error should name the failed unit: %v", err)
	}
	if !strings.Contains(buf.String(), "✗") || !strings.Contains(buf.String(), "bad.service") {
		t.Fatalf("failure not marked:\n%s", buf.String())
	}
}
