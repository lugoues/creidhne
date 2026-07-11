package systemd

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const canned = `Id=app.service
LoadState=loaded
ActiveState=active
SubState=running
NeedDaemonReload=no
ActiveEnterTimestamp=Fri 2026-07-11 10:00:00 UTC

Id=db.service
LoadState=loaded
ActiveState=failed
SubState=failed
NeedDaemonReload=yes
ActiveEnterTimestamp=

Id=gone.service
LoadState=not-found
ActiveState=inactive
SubState=dead
NeedDaemonReload=no
ActiveEnterTimestamp=
`

func TestParseShow(t *testing.T) {
	got := parseShow([]byte(canned), []string{"app.service", "db.service", "gone.service"})
	if len(got) != 3 {
		t.Fatalf("want 3 units, got %d: %v", len(got), got)
	}
	app := got["app.service"]
	if !app.Running() || app.NeedDaemonReload {
		t.Fatalf("app: %+v", app)
	}
	want := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	if !app.ActiveEnter.Equal(want) {
		t.Fatalf("app ActiveEnter = %v, want %v", app.ActiveEnter, want)
	}
	db := got["db.service"]
	if db.ActiveState != "failed" || !db.NeedDaemonReload || !db.ActiveEnter.IsZero() {
		t.Fatalf("db: %+v", db)
	}
	if gone := got["gone.service"]; gone.LoadState != "not-found" {
		t.Fatalf("gone: %+v", gone)
	}
}

// TestParseShowPositionalFallback: blocks without Id map to the requested unit
// at the same position.
func TestParseShowPositionalFallback(t *testing.T) {
	out := "LoadState=loaded\nActiveState=active\nSubState=running\n\nLoadState=not-found\n"
	got := parseShow([]byte(out), []string{"a.service", "b.service"})
	if got["a.service"].ActiveState != "active" || got["b.service"].LoadState != "not-found" {
		t.Fatalf("positional fallback wrong: %v", got)
	}
}

func TestParseTimestampLenient(t *testing.T) {
	for _, v := range []string{"", "n/a", "garbage"} {
		if ts := parseTimestamp(v); !ts.IsZero() {
			t.Fatalf("parseTimestamp(%q) = %v, want zero", v, ts)
		}
	}
}

// TestShowBatchesAndScopes: one systemctl call carries every unit, --user only
// when userScope is set, and the run error path degrades cleanly.
func TestShowBatchesAndScopes(t *testing.T) {
	orig := run
	defer func() { run = orig }()

	var gotArgs []string
	run = func(args ...string) ([]byte, error) {
		gotArgs = args
		return []byte(canned), nil
	}
	if _, err := Show(true, []string{"app.service", "db.service", "gone.service"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.HasPrefix(joined, "--user show --property=") || !strings.Contains(joined, "app.service db.service gone.service") {
		t.Fatalf("unexpected args: %v", gotArgs)
	}

	run = func(args ...string) ([]byte, error) { return nil, errors.New("systemctl: Failed to connect to bus") }
	if _, err := Show(false, []string{"x.service"}); err == nil {
		t.Fatal("expected error to propagate for degradation handling")
	}

	// Empty unit list never invokes systemctl.
	run = func(args ...string) ([]byte, error) { t.Fatal("run must not be called"); return nil, nil }
	if m, err := Show(false, nil); err != nil || len(m) != 0 {
		t.Fatalf("empty units: %v, %v", m, err)
	}
}
