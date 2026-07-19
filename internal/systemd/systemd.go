// Package systemd reads unit runtime state via systemctl. It is strictly
// read-only observability for `crei status`: reconciliation never consults it
// (files are the substrate; systemd's view is generator output derived from
// them). All units are fetched in one batched `systemctl show` call.
package systemd

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// UnitStatus is one unit's runtime state as systemd reports it.
type UnitStatus struct {
	Name             string
	LoadState        string // loaded, not-found, masked, ...
	ActiveState      string // active, inactive, failed, activating, ...
	SubState         string // running, dead, exited, ...
	NeedDaemonReload bool
	ActiveEnter      time.Time // zero when the unit never entered active
}

// Running reports the common healthy case.
func (u UnitStatus) Running() bool {
	return u.ActiveState == "active" && u.SubState == "running"
}

// properties requested from systemctl show, parsed by parseShow.
const properties = "Id,LoadState,ActiveState,SubState,NeedDaemonReload,ActiveEnterTimestamp"

// run executes systemctl and returns stdout; a var so tests can stub it.
var run = func(args ...string) ([]byte, error) {
	cmd := exec.Command("systemctl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && stdout.Len() == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("systemctl: %s", msg)
	}
	// Non-zero exit with usable stdout still parses: some systemctl versions
	// exit non-zero from `show` when a requested unit is not loaded, while
	// still printing its LoadState=not-found block.
	return stdout.Bytes(), nil
}

// Restart restarts the named units in one systemctl call. userScope selects
// `systemctl --user`.
func Restart(userScope bool, units []string) error {
	if len(units) == 0 {
		return nil
	}
	args := make([]string, 0, len(units)+2)
	if userScope {
		args = append(args, "--user")
	}
	args = append(args, "restart")
	args = append(args, units...)
	_, err := run(args...)
	return err
}

// Show fetches runtime state for the named units in one systemctl call.
// userScope selects `systemctl --user`. An empty unit list returns an empty
// map without invoking systemctl.
func Show(userScope bool, units []string) (map[string]UnitStatus, error) {
	if len(units) == 0 {
		return map[string]UnitStatus{}, nil
	}
	args := make([]string, 0, len(units)+3)
	if userScope {
		args = append(args, "--user")
	}
	args = append(args, "show", "--property="+properties)
	args = append(args, units...)
	out, err := run(args...)
	if err != nil {
		return nil, err
	}
	parsed := parseShow(out, units)
	if len(parsed) == 0 {
		// Units were requested but nothing parseable came back: a systemctl
		// shim or wrapper (common in containers) printing prose and exiting 0.
		// Treat as unavailable so status degrades with a note, not silence.
		return nil, fmt.Errorf("systemctl returned no unit properties: %s", firstNonEmptyLine(out))
	}
	return parsed, nil
}

func firstNonEmptyLine(out []byte) string {
	for _, l := range strings.Split(string(out), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return "(empty output)"
}

// parseShow decodes `systemctl show` output: one Key=Value block per unit,
// blocks separated by blank lines, emitted in argument order. Blocks are keyed
// by their Id property, falling back to argument position when Id is absent
// (e.g. a template or a very old systemd).
func parseShow(out []byte, requested []string) map[string]UnitStatus {
	result := make(map[string]UnitStatus, len(requested))
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	block := map[string]string{}
	idx := 0
	flush := func() {
		if len(block) == 0 {
			return
		}
		name := block["Id"]
		if name == "" && idx < len(requested) {
			name = requested[idx]
		}
		idx++
		if name == "" {
			block = map[string]string{}
			return
		}
		result[name] = UnitStatus{
			Name:             name,
			LoadState:        block["LoadState"],
			ActiveState:      block["ActiveState"],
			SubState:         block["SubState"],
			NeedDaemonReload: block["NeedDaemonReload"] == "yes",
			ActiveEnter:      parseTimestamp(block["ActiveEnterTimestamp"]),
		}
		block = map[string]string{}
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		block[k] = v
	}
	flush()
	return result
}

// parseTimestamp decodes systemctl's "Day YYYY-MM-DD HH:MM:SS ZONE" form.
// Parsed in the local location so the zone abbreviation systemctl printed
// (the machine's own zone, since status runs on the same host) resolves to a
// real offset. Empty, n/a, or unparseable values yield the zero time, which
// callers treat as "unknown" (no since column, no staleness marker).
func parseTimestamp(v string) time.Time {
	if v == "" || v == "n/a" {
		return time.Time{}
	}
	t, err := time.ParseInLocation("Mon 2006-01-02 15:04:05 MST", v, time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}
