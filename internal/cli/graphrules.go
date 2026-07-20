package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/lugoues/creidhne/internal/eval"
)

// pairMarkerPrefix is the pair-network contract label (placed by helpers such
// as extras' #ReverseProxyMixin): exactly two containers, the service and the
// proxy, should ever attach the carrying network. The marker is the whole
// contract; the rules need no knowledge of the helper that placed it.
const pairMarkerPrefix = "creidhne.pair="

// routerLabelPrefix keys traefik router definitions inside container labels;
// the same router name on two different units makes traefik merge or reject
// their config.
const routerLabelPrefix = "traefik.http.routers."

// ruleFinding is one whole-project invariant violation. Severity "error"
// fails validate; "warning" only reports.
type ruleFinding struct {
	Unit     string
	Severity string // "error" | "warning"
	Message  string
}

// graphRuleFindings checks invariants no single quadlet can express: #checks
// is quadlet-scoped and CUE cannot enumerate sibling quadlets, so
// cross-quadlet contracts are enforced here, over the evaluated manifests.
// Dangling network/volume references need no rule: the schema only admits
// them through #self handles, which resolve against real units at eval time.
func graphRuleFindings(all []eval.Quadlet) []ruleFinding {
	var out []ruleFinding

	// The project graph: every unit, plus who attaches each network.
	var networks []eval.UnitRecord
	var attachable []eval.UnitRecord   // containers and pods
	attachers := map[string][]string{} // network filename -> attaching unit filenames
	for _, q := range all {
		for _, u := range q.Units {
			switch u.Kind {
			case "network":
				networks = append(networks, u)
			case "container", "pod":
				attachable = append(attachable, u)
				for _, n := range resourceStrings(u.Data, "networkStrings") {
					ref := firstField(n)
					if strings.HasSuffix(ref, ".network") {
						attachers[ref] = append(attachers[ref], u.Filename)
					}
				}
			}
		}
	}

	// Pair cardinality: more than two attachers breaks the isolation
	// contract (error); fewer than two is incomplete wiring, legitimate
	// mid-migration (warning).
	for _, n := range networks {
		pair := ""
		for _, l := range topList(n.Data, "labelStrings") {
			if v, ok := strings.CutPrefix(l, pairMarkerPrefix); ok {
				pair = v
			}
		}
		if pair == "" {
			continue
		}
		at := attachers[n.Filename]
		switch {
		case len(at) > 2:
			out = append(out, ruleFinding{Unit: n.Filename, Severity: "error",
				Message: fmt.Sprintf("pair network (creidhne.pair=%s) has %d attachers (%s); the contract is exactly two: the service and the proxy", pair, len(at), strings.Join(at, ", "))})
		case len(at) < 2:
			out = append(out, ruleFinding{Unit: n.Filename, Severity: "warning",
				Message: fmt.Sprintf("pair network (creidhne.pair=%s) has %d attacher(s); expected the service and the proxy — wiring incomplete?", pair, len(at))})
		}
	}

	// Duplicate effective runtime names: podman rejects the second container,
	// and helpers (hosts books) resolve names to the wrong peer.
	names := map[string][]string{}
	for _, u := range attachable {
		name := ""
		switch u.Kind {
		case "container":
			name = nestedStr(u.Data, "Container", "ContainerName")
		case "pod":
			name = nestedStr(u.Data, "Pod", "PodName")
		}
		if name == "" {
			name = "systemd-" + u.Stem
		}
		names[name] = append(names[name], u.Filename)
	}
	for name, units := range names {
		if len(units) > 1 {
			sort.Strings(units)
			out = append(out, ruleFinding{Unit: units[0], Severity: "error",
				Message: fmt.Sprintf("effective runtime name %q is shared by %s; podman requires uniqueness and name-based references resolve arbitrarily", name, strings.Join(units, ", "))})
		}
	}

	// Orphan networks: defined but never attached by any unit in the
	// project. Pair-marked networks are excluded (the cardinality rule
	// already reports them more precisely).
	for _, n := range networks {
		isPair := false
		for _, l := range topList(n.Data, "labelStrings") {
			if strings.HasPrefix(l, pairMarkerPrefix) {
				isPair = true
			}
		}
		if !isPair && len(attachers[n.Filename]) == 0 {
			out = append(out, ruleFinding{Unit: n.Filename, Severity: "warning",
				Message: "no container or pod in the project attaches this network"})
		}
	}

	// Duplicate traefik router names across units: labels merge in traefik's
	// docker provider, silently splicing two services' route config.
	routers := map[string]map[string]bool{}
	for _, u := range attachable {
		for _, l := range topList(u.Data, "labelStrings") {
			rest, ok := strings.CutPrefix(strings.Trim(l, "'"), routerLabelPrefix)
			if !ok {
				continue
			}
			name, _, ok := strings.Cut(rest, ".")
			if !ok {
				continue
			}
			if routers[name] == nil {
				routers[name] = map[string]bool{}
			}
			routers[name][u.Filename] = true
		}
	}
	for name, units := range routers {
		if len(units) > 1 {
			ids := make([]string, 0, len(units))
			for id := range units {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			out = append(out, ruleFinding{Unit: ids[0], Severity: "warning",
				Message: fmt.Sprintf("traefik router %q is defined by %s; router labels merge across containers and corrupt both routes", name, strings.Join(ids, ", "))})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity == "error"
		}
		if out[i].Unit != out[j].Unit {
			return out[i].Unit < out[j].Unit
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// nestedStr reads a string under data[section][key] ("" when absent).
func nestedStr(data map[string]any, section, key string) string {
	sec, ok := data[section].(map[string]any)
	if !ok {
		return ""
	}
	s, _ := sec[key].(string)
	return s
}

// printRuleFindings renders findings grouped by unit and returns the error
// count (the caller decides whether errors are fatal for its command).
func printRuleFindings(out io.Writer, findings []ruleFinding) (errs int) {
	cur := ""
	for _, f := range findings {
		if f.Unit != cur {
			cur = f.Unit
			fmt.Fprintln(out, cur)
		}
		tag := yellow("warning:")
		if f.Severity == "error" {
			tag = red("error:")
			errs++
		}
		fmt.Fprintln(out, "  "+tag+" "+f.Message)
	}
	return errs
}
