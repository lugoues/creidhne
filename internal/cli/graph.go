package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
)

// unitDirectives are the [Unit] dependency directives whose values name other
// systemd units; each becomes a labeled edge in the graph.
var unitDirectives = []string{
	"After", "Before", "Requires", "Requisite", "Wants",
	"BindsTo", "PartOf", "Upholds", "Conflicts", "OnFailure", "OnSuccess",
}

// graphNode is a unit (or an external systemd unit referenced by one).
type graphNode struct {
	ID       string `json:"id"`                // the unit filename (e.g. "app.container"), or a raw external unit name
	Kind     string `json:"kind"`              // container/pod/volume/network/build/image/kube/artifact, or "external"
	Quadlet  string `json:"quadlet,omitempty"` // owning #Quadlet name; empty for external nodes
	External bool   `json:"external"`
}

// graphEdge is a declared dependency from one unit to another, labeled by the
// relationship that induced it (a [Unit] directive, or image/pod/network/volume).
type graphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rel  string `json:"rel"`
}

type depGraph struct {
	nodes map[string]graphNode
	edges []graphEdge
}

func newGraphCmd() *cobra.Command {
	var format string
	var flat bool
	cmd := &cobra.Command{
		Use:   "graph [quadlet...]",
		Short: "Print the declared dependency graph (dot, mermaid, or json)",
		Long: "graph prints the dependency graph crei can see from the definitions:\n" +
			"[Unit] ordering/requirement directives (After, Requires, ...) plus\n" +
			"resource coupling (a container's Image=, its pod, networks, and volumes).\n\n" +
			"With no arguments it graphs the whole project; given quadlet names it\n" +
			"graphs only those. It shows *declared* coupling only, not runtime edges\n" +
			"(e.g. one service calling another over the network).\n\n" +
			"Nodes are clustered by their owning quadlet; --flat disables that.\n\n" +
			"Formats: dot (pipe to graphviz, e.g. 'crei graph | dot -Tsvg > g.svg'),\n" +
			"mermaid (renders on GitHub or mermaid.live), or json.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			all, err := loadQuadlets(cfg.ProjectDir)
			if err != nil {
				return err
			}
			if len(all) == 0 {
				return fmt.Errorf("no quadlets found (no top-level #Quadlet values in %s)", cfg.ProjectDir)
			}
			if err := checkUniqueFilenames(all); err != nil {
				return err
			}
			focus := all
			if len(args) > 0 {
				focus, err = filterQuadlets(all, args)
				if err != nil {
					return err
				}
			}
			g := buildGraph(focus, all)
			out := cmd.OutOrStdout()
			switch format {
			case "dot":
				writeDot(out, g, !flat)
			case "mermaid":
				writeMermaid(out, g, !flat)
			case "json":
				return writeGraphJSON(out, g)
			default:
				return fmt.Errorf("invalid --format %q (want dot, mermaid, or json)", format)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "dot", "output format: dot, mermaid, or json")
	cmd.Flags().BoolVar(&flat, "flat", false, "don't cluster nodes by quadlet (dot and mermaid only)")
	return cmd
}

// checkUniqueFilenames guards against two units resolving to the same on-disk
// filename, which would silently merge nodes (wrong kind/quadlet tag, a dropped
// unit). render.BuildFileSet rejects this at apply time; graph reads eval data
// directly and would otherwise misrender, so it enforces the same invariant.
func checkUniqueFilenames(quads []eval.Quadlet) error {
	owner := map[string]string{}
	for _, q := range quads {
		for _, u := range q.Units {
			if prev, ok := owner[u.Filename]; ok {
				return fmt.Errorf("duplicate output file %q: emitted by both quadlet %q and quadlet %q", u.Filename, prev, q.Name)
			}
			owner[u.Filename] = q.Name
		}
	}
	return nil
}

// buildGraph builds the dependency graph for the focus quadlets. References are
// resolved against the whole project (all), so a focus unit's edge to a unit in
// another quadlet still resolves and pulls that unit in as a node -- the
// neighborhood view. When focus == all (no filter) this is just the full graph.
func buildGraph(focus, all []eval.Quadlet) depGraph {
	g := depGraph{nodes: map[string]graphNode{}}
	catalog := map[string]graphNode{} // filename -> node (kind + quadlet), every unit in the project
	svcToID := map[string]string{}    // "app.service" -> "app.container"
	for _, q := range all {
		for _, u := range q.Units {
			catalog[u.Filename] = graphNode{ID: u.Filename, Kind: u.Kind, Quadlet: q.Name}
			if u.Service != "" {
				svcToID[u.Service] = u.Filename
			}
		}
	}
	for _, q := range focus {
		for _, u := range q.Units {
			g.nodes[u.Filename] = catalog[u.Filename]
		}
	}
	for _, q := range focus {
		for _, u := range q.Units {
			g.addUnitEdges(u, svcToID, catalog)
			g.addResourceEdges(u, catalog)
		}
	}
	return g
}

// ensureManaged draws a referenced unit (with its real kind and quadlet, from
// the catalog) if the focus didn't already include it -- the neighborhood view.
func (g *depGraph) ensureManaged(id string, catalog map[string]graphNode) {
	if _, ok := g.nodes[id]; !ok {
		g.nodes[id] = catalog[id]
	}
}

// ensureExternal adds an ungrouped external node for an unmanaged target.
func (g *depGraph) ensureExternal(id string) {
	if _, ok := g.nodes[id]; !ok {
		g.nodes[id] = graphNode{ID: id, Kind: "external", External: true}
	}
}

// addUnitEdges adds an edge for each [Unit] dependency directive. A target that
// names a managed unit resolves to it; an unmanaged one (e.g.
// network-online.target) becomes a distinct external node so the declared
// dependency is still visible.
func (g *depGraph) addUnitEdges(u eval.UnitRecord, svcToID map[string]string, catalog map[string]graphNode) {
	for _, directive := range unitDirectives {
		for _, target := range nestedList(u.Data, "Unit", directive) {
			to := target
			if id, ok := svcToID[target]; ok {
				to = id
				g.ensureManaged(id, catalog)
			} else {
				g.ensureExternal(target)
			}
			g.edges = append(g.edges, graphEdge{From: u.Filename, To: to, Rel: directive})
		}
	}
}

// addResourceEdges adds edges for a container's image (when it consumes a
// managed .build/.image), its pod, its networks, and its volumes. Only managed
// units become edges; raw registry images, host paths, and network modes (host,
// container:...) are not graph nodes.
func (g *depGraph) addResourceEdges(u eval.UnitRecord, catalog map[string]graphNode) {
	link := func(ref, rel string) {
		if _, ok := catalog[ref]; ok {
			g.ensureManaged(ref, catalog)
			g.edges = append(g.edges, graphEdge{From: u.Filename, To: ref, Rel: rel})
		}
	}
	if img := topStr(u.Data, "imageString"); img != "" {
		link(img, "image")
	}
	if pod := topStr(u.Data, "podString"); pod != "" {
		link(pod, "pod")
	}
	for _, n := range resourceStrings(u.Data, "networkStrings") {
		link(firstField(n), "network")
	}
	for _, v := range resourceStrings(u.Data, "volumeStrings") {
		link(firstField(v), "volume")
	}
}

// resourceStrings reads a flattened ref list that lives at the top level for
// container/pod units but is nested under "Build" for build units (build.cue
// defines networkStrings/volumeStrings inside the Build section). A unit has
// them in exactly one place, so top-level-first with a Build fallback is exact.
func resourceStrings(data map[string]any, key string) []string {
	if s := topList(data, key); len(s) > 0 {
		return s
	}
	return nestedList(data, "Build", key)
}

// firstField returns the token before the first ':' (a volume/network ref is
// "<ref>:target:opts" or "<ref>:alias=...").
func firstField(s string) string {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[:i]
	}
	return s
}

// --- ordered accessors over the decoded unit data ---

func topStr(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return s
}

func topList(data map[string]any, key string) []string {
	return toStrings(data[key])
}

func nestedList(data map[string]any, section, key string) []string {
	sec, ok := data[section].(map[string]any)
	if !ok {
		return nil
	}
	return toStrings(sec[key])
}

func toStrings(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// sortedNodeIDs and sortedEdges give deterministic output.
func (g depGraph) sortedNodeIDs() []string {
	ids := make([]string, 0, len(g.nodes))
	for id := range g.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// quadletGroups partitions nodes by owning quadlet for clustered rendering:
// the sorted quadlet names, each mapped to its sorted node ids, plus the sorted
// ungrouped (external) node ids. Relies on sortedNodeIDs for stable order.
func (g depGraph) quadletGroups() (order []string, byQuadlet map[string][]string, ungrouped []string) {
	byQuadlet = map[string][]string{}
	for _, id := range g.sortedNodeIDs() {
		q := g.nodes[id].Quadlet
		if q == "" {
			ungrouped = append(ungrouped, id)
			continue
		}
		if _, seen := byQuadlet[q]; !seen {
			order = append(order, q)
		}
		byQuadlet[q] = append(byQuadlet[q], id)
	}
	sort.Strings(order)
	return order, byQuadlet, ungrouped
}

func (g depGraph) sortedEdges() []graphEdge {
	edges := append([]graphEdge(nil), g.edges...)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].Rel != edges[j].Rel {
			return edges[i].Rel < edges[j].Rel
		}
		return edges[i].To < edges[j].To
	})
	// Drop byte-identical duplicate edges (same from/to/rel), which a repeated
	// reference (e.g. After listing a target twice) would otherwise produce.
	out := edges[:0]
	for _, e := range edges {
		if n := len(out); n > 0 && out[n-1] == e {
			continue
		}
		out = append(out, e)
	}
	return out
}
