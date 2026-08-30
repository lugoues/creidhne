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
