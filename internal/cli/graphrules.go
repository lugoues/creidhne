package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/lugoues/creidhne/internal/eval"
)

// pairMarkerPrefix is the pair-network contract label (placed by helpers such
// as extras' #ReverseProxyMixin): the owning quadlet's own units (one or
// several route-serving containers) plus exactly one external attacher, the
// proxy. The marker is the whole contract; the rules need no knowledge of
// the helper that placed it.
const pairMarkerPrefix = "creidhne.pair="

// routerFamilies are the traefik label families whose router definitions the
// duplicate-router rule scans (udp is unmodeled in the helpers). Router names
// are namespaced per family — traefik keeps http and tcp routers separate, so
// the same name across families is legal and only a same-family duplicate
// merges config.
var routerFamilies = []string{"http", "tcp"}

// ruleFinding is one named-rule violation. Findings are created with Rule set
// and Severity empty; lintLevels.apply stamps the effective severity (config
// override or the rule's default) and drops "off" rules. Severity "error"
// fails validate; "warn" only reports.
type ruleFinding struct {
	Rule     string // e.g. "graph/orphan-network"
	Unit     string
	Severity string // stamped by lintLevels.apply
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
	attached := map[string]bool{}      // "network|unit" — dedupe: one unit is one attacher
	owner := map[string]string{}       // unit filename -> owning quadlet
	attach := func(ref, unit string) {
		if !strings.HasSuffix(ref, ".network") || attached[ref+"|"+unit] {
			return
		}
		attached[ref+"|"+unit] = true
		attachers[ref] = append(attachers[ref], unit)
	}
	for _, q := range all {
		for _, u := range q.Units {
			owner[u.Filename] = q.Name
			switch u.Kind {
			case "network":
				networks = append(networks, u)
			case "container", "pod":
				attachable = append(attachable, u)
				for _, n := range resourceStrings(u.Data, "networkStrings") {
					attach(firstField(n), u.Filename)
				}
			case "kube":
				// Kube units attach networks through raw [Kube] Network=
				// strings; without them a network used only by a kube would
				// misreport as orphaned (or as missing its pair attacher).
				for _, n := range nestedList(u.Data, "Kube", "Network") {
					attach(firstField(n), u.Filename)
				}
			}
		}
	}

	// Pair contract, ownership-based: the owning quadlet's units may all
	// attach (per-container route splitting is a documented pattern), but
	// exactly one external unit, the proxy, may join them. A second foreign
	// attacher is the isolation breach (error); none yet, or no in-quadlet
	// service, is incomplete wiring (warning). Route-to-container counts
	// are not derivable here: routes can share one container.
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
		var external, internal []string
		for _, at := range attachers[n.Filename] {
			if owner[at] == owner[n.Filename] {
				internal = append(internal, at)
			} else {
				external = append(external, at)
			}
		}
		switch {
		case len(external) > 1:
			out = append(out, ruleFinding{Rule: "graph/pair-cardinality", Unit: n.Filename,
				Message: fmt.Sprintf("pair network (creidhne.pair=%s) has %d external attachers (%s); the contract allows exactly one, the proxy", pair, len(external), strings.Join(external, ", "))})
		case len(external) == 0:
			out = append(out, ruleFinding{Rule: "graph/pair-unwired", Unit: n.Filename,
				Message: fmt.Sprintf("pair network (creidhne.pair=%s) has no external attacher; the proxy is not wired yet", pair)})
		case len(internal) == 0:
			out = append(out, ruleFinding{Rule: "graph/pair-unwired", Unit: n.Filename,
				Message: fmt.Sprintf("pair network (creidhne.pair=%s) has no in-quadlet attacher; nothing serves behind the proxy", pair)})
		}
	}

	// Duplicate effective runtime names: podman rejects the second container,
	// and helpers (hosts books) resolve names to the wrong peer. Pods and
	// containers are separate name spaces in libpod (only IDs are shared),
	// so a pod and a container may share a name; what does collide is a
	// pod's infra container, which podman names "<pod>-infra".
	ctrNames := map[string][]string{}
	podNames := map[string][]string{}
	for _, u := range attachable {
		switch u.Kind {
		case "container":
			name := nestedStr(u.Data, "Container", "ContainerName")
			if name == "" {
				name = "systemd-" + u.Stem
			}
			ctrNames[name] = append(ctrNames[name], u.Filename)
		case "pod":
			name := nestedStr(u.Data, "Pod", "PodName")
			if name == "" {
				name = "systemd-" + u.Stem
			}
			podNames[name] = append(podNames[name], u.Filename)
			if infra := podInfraName(u.Data, name); infra != "" {
				ctrNames[infra] = append(ctrNames[infra], u.Filename)
			}
		}
	}
	for _, names := range []map[string][]string{ctrNames, podNames} {
		for name, units := range names {
			if len(units) > 1 {
				sort.Strings(units)
				out = append(out, ruleFinding{Rule: "graph/duplicate-name", Unit: units[0],
					Message: fmt.Sprintf("effective runtime name %q is shared by %s; podman requires uniqueness and name-based references resolve arbitrarily", name, strings.Join(units, ", "))})
			}
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
			out = append(out, ruleFinding{Rule: "graph/orphan-network", Unit: n.Filename,
				Message: "no container or pod in the project attaches this network"})
		}
	}

	// Duplicate traefik router names across units: labels merge in traefik's
	// docker provider, silently splicing two services' route config. Keyed
	// "family/name" so http and tcp routers stay separate namespaces, as they
	// are in traefik.
	routers := map[string]map[string]bool{}
	for _, u := range attachable {
		for _, l := range topList(u.Data, "labelStrings") {
			clean := strings.Trim(l, "'")
			for _, family := range routerFamilies {
				rest, ok := strings.CutPrefix(clean, "traefik."+family+".routers.")
				if !ok {
					continue
				}
				name, _, ok := strings.Cut(rest, ".")
				if !ok {
					continue
				}
				key := family + "/" + name
				if routers[key] == nil {
					routers[key] = map[string]bool{}
				}
				routers[key][u.Filename] = true
			}
		}
	}
	for key, units := range routers {
		if len(units) > 1 {
			ids := make([]string, 0, len(units))
			for id := range units {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			family, name, _ := strings.Cut(key, "/")
			out = append(out, ruleFinding{Rule: "graph/duplicate-router", Unit: ids[0],
				Message: fmt.Sprintf("traefik %s router %q is defined by %s; router labels merge across containers and corrupt both routes", family, name, strings.Join(ids, ", "))})
		}
	}

	return out
}

// sortFindings orders stamped findings: errors first, then by unit, then
// message. Call after lintLevels.apply has assigned severities.
func sortFindings(fs []ruleFinding) {
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].Severity != fs[j].Severity {
			return fs[i].Severity == sevError
		}
		if fs[i].Unit != fs[j].Unit {
			return fs[i].Unit < fs[j].Unit
		}
		return fs[i].Message < fs[j].Message
	})
}

// nestedStr reads a string under data[section][key] ("" when absent).
// podInfraName returns the name podman gives the pod's infra container:
// "<pod>-infra" unless PodmanArgs renames it (--infra-name) or disables it
// (--infra=false), in which case it returns "".
func podInfraName(data map[string]any, pod string) string {
	infra := pod + "-infra"
	sec, _ := data["Pod"].(map[string]any)
	var args []string
	var flatten func(v any)
	flatten = func(v any) {
		switch v := v.(type) {
		case string:
			args = append(args, strings.Fields(v)...)
		case []any:
			for _, e := range v {
				flatten(e)
			}
		}
	}
	flatten(sec["PodmanArgs"])
	for i, a := range args {
		switch {
		case a == "--infra=false" || a == "--no-infra":
			return ""
		case a == "--infra" && i+1 < len(args) && args[i+1] == "false":
			return ""
		case strings.HasPrefix(a, "--infra-name="):
			infra = strings.TrimPrefix(a, "--infra-name=")
		case a == "--infra-name" && i+1 < len(args):
			infra = args[i+1]
		}
	}
	return infra
}

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
		if f.Severity == sevError {
			tag = red("error:")
			errs++
		}
		fmt.Fprintln(out, "  "+tag+" "+f.Message+" "+dim("("+f.Rule+")"))
	}
	return errs
}
