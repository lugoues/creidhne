package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lugoues/creidhne/internal/eval"
)

// Stop-timeout coherence: podman's grace period (StopTimeout, default 10s)
// must stay below systemd's stop timeout (TimeoutStopSec, default 90s on
// stock hosts), or systemd SIGKILLs podman mid-cleanup when a stop runs the
// full grace period. Two rules because only the both-explicit case is
// provable; the others compare against assumed defaults the host can change.
const (
	podmanDefaultGrace = 10
	systemdDefaultStop = 90
	stopHeadroomAdvice = "leave headroom for kill+cleanup, e.g. StopTimeout+60"
)

// timeoutRuleFindings checks each container/pod unit's StopTimeout against
// its [Service] stop timeout.
func timeoutRuleFindings(focus []eval.Quadlet) []ruleFinding {
	var out []ruleFinding
	for _, q := range focus {
		for _, u := range q.Units {
			// Restart= means the same thing in any unit's [Service], so this
			// runs ahead of the container/pod guard the stop-timeout rules
			// need: a .kube retries just as fast, and even a oneshot .build
			// accepts Restart=on-failure.
			out = append(out, startupRuleFindings(u)...)
			if u.Kind != "container" && u.Kind != "pod" {
				continue
			}
			grace, graceSet := stopGrace(u)
			stop, stopKey, stopSet, infinite := serviceStopTimeout(u.Data)
			switch {
			case infinite:
				// TimeoutStopSec=infinity can never undercut the grace period.
			case graceSet && stopSet:
				if stop <= float64(grace) {
					out = append(out, ruleFinding{Rule: "service/stop-timeout", Unit: u.Filename,
						Message: fmt.Sprintf("%s (%ss) does not exceed StopTimeout (%ds): systemd kills podman before the grace period completes; %s", stopKey, trimFloat(stop), grace, stopHeadroomAdvice)})
				}
			case graceSet:
				if grace >= systemdDefaultStop {
					out = append(out, ruleFinding{Rule: "service/stop-timeout-default", Unit: u.Filename,
						Message: fmt.Sprintf("StopTimeout (%ds) meets or exceeds systemd's default stop timeout (DefaultTimeoutStopSec, typically %ds): set Service.TimeoutStopSec above it; %s", grace, systemdDefaultStop, stopHeadroomAdvice)})
				}
			case stopSet:
				if stop <= podmanDefaultGrace {
					out = append(out, ruleFinding{Rule: "service/stop-timeout-default", Unit: u.Filename,
						Message: fmt.Sprintf("%s (%ss) does not exceed podman's default grace period (%ds): systemd kills podman before the grace period completes; set StopTimeout below it or raise %s", stopKey, trimFloat(stop), podmanDefaultGrace, stopKey)})
				}
			}
		}
	}
	return out
}

// restartingModes are the Restart= values that make systemd relaunch the unit,
// so the RestartSec delay between attempts actually matters.
var restartingModes = map[string]bool{
	"always": true, "on-success": true, "on-failure": true,
	"on-abnormal": true, "on-watchdog": true, "on-abort": true,
}

// oneshotModes are the two Restart= values systemd refuses outright on a
// Type=oneshot service ("Refusing", at unit load).
var oneshotModes = map[string]bool{"always": true, "on-success": true}

// oneshotKinds are the unit kinds quadlet generates as Type=oneshot, so their
// [Service] Type is oneshot unless the unit overrides it.
var oneshotKinds = map[string]bool{
	"build": true, "image": true, "network": true, "volume": true, "artifact": true,
}

// isOneshot reports the unit's effective service type as quadlet will
// generate it: an explicit Type= survives for every kind except pods, which
// ConvertPod unconditionally rewrites to forking.
func isOneshot(u eval.UnitRecord, svc map[string]any) bool {
	if u.Kind == "pod" {
		return false
	}
	if t, ok := svc["Type"].(string); ok {
		return t == "oneshot"
	}
	return oneshotKinds[u.Kind]
}

// startupRuleFindings checks the two [Service] settings whose absence quietly
// hands start-up and crash-loop behavior to a host default the project cannot
// see. Both are keyed on something the unit itself declares, so neither fires
// on a container that never asked for the coupling.
func startupRuleFindings(u eval.UnitRecord) []ruleFinding {
	var out []ruleFinding
	svc, _ := u.Data["Service"].(map[string]any)

	// Notify=healthy withholds READY until the first healthcheck passes, so
	// start-up is bounded by the health cadence rather than by the process
	// launching, and DefaultTimeoutStartSec (a host setting, not the
	// project's) decides when a never-healthy container is killed.
	if u.Kind == "container" && !hasServiceKey(svc, "TimeoutStartSec", "TimeoutSec") {
		if con, _ := u.Data["Container"].(map[string]any); con["Notify"] == "healthy" {
			out = append(out, ruleFinding{Rule: "service/start-timeout", Unit: u.Filename,
				Message: "Notify=healthy gates start-up on the healthcheck but no TimeoutStartSec is set: a container that never goes healthy burns the host's DefaultTimeoutStartSec (typically 90s); set it from HealthStartPeriod plus a HealthInterval or two"})
		}
	}

	mode, hasMode := svc["Restart"].(string)
	switch {
	case !hasMode || !restartingModes[mode]:
		// Restart=no (or unset) never relaunches.
	case oneshotModes[mode] && isOneshot(u, svc):
		// No RestartSec advice here: systemd refuses to load the unit at all,
		// so the delay between attempts is not the problem.
		out = append(out, ruleFinding{Rule: "service/oneshot-restart", Unit: u.Filename,
			Message: fmt.Sprintf("Restart=%s is rejected on a Type=oneshot service: systemd refuses to load the unit; use on-failure/on-abnormal/on-abort, or set an explicit non-oneshot Type", mode)})
	case !hasServiceKey(svc, "RestartSec"):
		// systemd's RestartSec default is 100ms, so a unit that fails on
		// startup relaunches as fast as podman can go, burying the real error.
		out = append(out, ruleFinding{Rule: "service/restart-delay", Unit: u.Filename,
			Message: fmt.Sprintf("Restart=%s without RestartSec: systemd's 100ms default relaunches a failing unit as fast as podman allows, flooding the journal; set RestartSec (5s is a reasonable floor)", mode)})
	default:
		if f := restartFlapFinding(u, svc, mode); f != nil {
			out = append(out, *f)
		}
	}
	return out
}

// systemd's start rate limiter defaults (DefaultStartLimitIntervalSec,
// DefaultStartLimitBurst): host settings, assumed here like the stop-timeout
// defaults are.
const (
	systemdDefaultStartLimitInterval = 10.0
	systemdDefaultStartLimitBurst    = 5
)

// restartFlapFinding checks that a relaunching unit can still hit the start
// rate limiter. The limiter trips when StartLimitBurst starts land inside
// StartLimitIntervalSec; with RestartSec between attempts the burst-th start
// comes no sooner than (burst-1)*RestartSec, so once that meets the window a
// permanently failing unit never reaches "failed": it flaps in
// "activating (auto-restart)" until someone notices. An explicit 0/infinity
// interval, or burst 0, disables the limiter on purpose and is left alone.
func restartFlapFinding(u eval.UnitRecord, svc map[string]any, mode string) *ruleFinding {
	delay, ok := timeSpanField(svc["RestartSec"])
	if !ok {
		return nil
	}
	unit, _ := u.Data["Unit"].(map[string]any)
	interval, intervalSrc := systemdDefaultStartLimitInterval, "systemd default"
	if v, set := unit["StartLimitIntervalSec"]; set {
		iv, ok := timeSpanField(v)
		if !ok || iv == 0 {
			return nil // unparseable (leave to systemd) or deliberately disabled
		}
		interval, intervalSrc = iv, "explicit"
	}
	burst := int64(systemdDefaultStartLimitBurst)
	if v, set := unit["StartLimitBurst"].(int64); set {
		if v == 0 {
			return nil
		}
		burst = v
	}
	span := float64(burst-1) * delay
	if span < interval {
		return nil
	}
	return &ruleFinding{Rule: "service/restart-flap", Unit: u.Filename,
		Message: fmt.Sprintf("Restart=%s with RestartSec=%ss can never trip the start rate limiter (StartLimitBurst %d within StartLimitIntervalSec %ss, %s): the %d gaps alone span %ss, so a permanently failing unit restarts forever in activating (auto-restart) and never reaches failed; set Unit.StartLimitIntervalSec above that, e.g. 300s",
			mode, trimFloat(delay), burst, trimFloat(interval), intervalSrc, burst-1, trimFloat(span))}
}

// timeSpanField reads a #TimeSpan-typed field as seconds: a bare integer
// means seconds, a string is a systemd time span, "infinity" is unbounded
// (reported as not-ok so callers treat it as "no finite value").
func timeSpanField(v any) (float64, bool) {
	switch v := v.(type) {
	case int64:
		return float64(v), true
	case string:
		if v == "infinity" {
			return 0, false
		}
		return parseTimeSpan(v)
	}
	return 0, false
}

// hasServiceKey reports whether [Service] sets any of keys. Presence is the
// whole test: an explicit "infinity" or 0 is a deliberate choice, not an
// omission.
func hasServiceKey(svc map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := svc[k]; ok {
			return true
		}
	}
	return false
}

// stopGrace returns the unit's effective podman grace period in seconds.
// Container PodmanArgs may override via --stop-timeout (podman run); pod
// PodmanArgs go to podman pod create, which has no such flag.
func stopGrace(u eval.UnitRecord) (int64, bool) {
	section := map[string]string{"container": "Container", "pod": "Pod"}[u.Kind]
	sec, _ := u.Data[section].(map[string]any)
	grace, set := int64(0), false
	if v, ok := sec["StopTimeout"].(int64); ok {
		grace, set = v, true
	}
	if u.Kind == "container" {
		args := flattenArgs(sec["PodmanArgs"])
		for i, a := range args {
			switch {
			case strings.HasPrefix(a, "--stop-timeout="):
				if v, err := strconv.ParseInt(trimQuotes(strings.TrimPrefix(a, "--stop-timeout=")), 10, 64); err == nil {
					grace, set = v, true
				}
			case a == "--stop-timeout" && i+1 < len(args):
				if v, err := strconv.ParseInt(trimQuotes(args[i+1]), 10, 64); err == nil {
					grace, set = v, true
				}
			}
		}
	}
	return grace, set
}

// serviceStopTimeout reads the [Service] stop timeout: TimeoutStopSec, or the
// TimeoutSec shorthand (which sets both start and stop) when only it is given.
func serviceStopTimeout(data map[string]any) (secs float64, key string, set, infinite bool) {
	sec, _ := data["Service"].(map[string]any)
	for _, k := range []string{"TimeoutStopSec", "TimeoutSec"} {
		v, ok := sec[k]
		if !ok {
			continue
		}
		if v, ok := v.(string); ok {
			if v == "infinity" {
				return 0, k, false, true
			}
			if s, ok := parseTimeSpan(v); ok {
				// A zero span disables the timeout (parse_sec_fix_0
				// semantics, SysV compat), same as infinity.
				if s == 0 {
					return 0, k, false, true
				}
				return s, k, true, false
			}
		}
		return 0, "", false, false // unparseable: leave it to systemd
	}
	return 0, "", false, false
}

// spanUnits maps systemd time-span suffixes to seconds, longest-match first
// (config_parse_sec's table, sub-second units rounded into fractions).
var spanUnits = []struct {
	suffix string
	secs   float64
}{
	{"seconds", 1}, {"second", 1}, {"sec", 1},
	{"minutes", 60}, {"minute", 60}, {"min", 60},
	{"months", 2629800}, {"month", 2629800},
	{"msec", 0.001}, {"ms", 0.001},
	{"usec", 0.000001}, {"us", 0.000001}, {"µs", 0.000001}, {"μs", 0.000001},
	{"hours", 3600}, {"hour", 3600}, {"hr", 3600},
	{"days", 86400}, {"day", 86400},
	{"weeks", 604800}, {"week", 604800},
	{"years", 31557600}, {"year", 31557600},
	{"m", 60}, {"s", 1}, {"h", 3600}, {"d", 86400}, {"w", 604800}, {"M", 2629800}, {"y", 31557600},
}

// parseTimeSpan parses a systemd time span ("90", "1min 30s", "1.5h") into
// seconds. A bare number means seconds.
func parseTimeSpan(s string) (float64, bool) {
	rest := strings.TrimSpace(s)
	if rest == "" {
		return 0, false
	}
	total, matched := 0.0, false
	for rest != "" {
		rest = strings.TrimLeft(rest, " \t")
		i := 0
		for i < len(rest) && (rest[i] >= '0' && rest[i] <= '9' || rest[i] == '.') {
			i++
		}
		if i == 0 {
			return 0, false
		}
		val, err := strconv.ParseFloat(rest[:i], 64)
		if err != nil {
			return 0, false
		}
		rest = strings.TrimLeft(rest[i:], " \t")
		unit := 1.0 // bare number: seconds
		for _, u := range spanUnits {
			if strings.HasPrefix(rest, u.suffix) {
				unit = u.secs
				rest = rest[len(u.suffix):]
				break
			}
		}
		total += val * unit
		matched = true
	}
	return total, matched
}

// trimFloat renders seconds without a trailing ".0" noise.
func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
