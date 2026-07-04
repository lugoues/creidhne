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
	ID       string `json:"id"`   // the unit filename (e.g. "app.container"), or a raw external unit name
	Kind     string `json:"kind"` // container/pod/volume/network/build/image/kube/artifact, or "external"
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
	cmd := &cobra.Command{
		Use:   "graph [quadlet...]",
		Short: "Print the declared dependency graph (dot, mermaid, or json)",
		Long: "graph prints the dependency graph crei can see from the definitions:\n" +
			"[Unit] ordering/requirement directives (After, Requires, ...) plus\n" +
			"resource coupling (a container's Image=, its pod, networks, and volumes).\n\n" +
			"With no arguments it graphs the whole project; given quadlet names it\n" +
			"graphs only those. It shows *declared* coupling only, not runtime edges\n" +
			"(e.g. one service calling another over the network).\n\n" +
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
				writeDot(out, g)
			case "mermaid":
				writeMermaid(out, g)
			case "json":
				return writeGraphJSON(out, g)
			default:
				return fmt.Errorf("invalid --format %q (want dot, mermaid, or json)", format)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "dot", "output format: dot, mermaid, or json")
	return cmd
}

// buildGraph builds the dependency graph for the focus quadlets. References are
// resolved against the whole project (all), so a focus unit's edge to a unit in
// another quadlet still resolves and pulls that unit in as a node -- the
// neighborhood view. When focus == all (no filter) this is just the full graph.
func buildGraph(focus, all []eval.Quadlet) depGraph {
	g := depGraph{nodes: map[string]graphNode{}}
	svcToID := map[string]string{} // "app.service" -> "app.container"
	refToID := map[string]string{} // "app.container" -> "app.container"
	idKind := map[string]string{}  // "app.container" -> "container"
	for _, q := range all {
		for _, u := range q.Units {
			refToID[u.Filename] = u.Filename
			idKind[u.Filename] = u.Kind
			if u.Service != "" {
				svcToID[u.Service] = u.Filename
			}
		}
	}
	for _, q := range focus {
		for _, u := range q.Units {
			g.nodes[u.Filename] = graphNode{ID: u.Filename, Kind: u.Kind}
		}
	}
	for _, q := range focus {
		for _, u := range q.Units {
			g.addUnitEdges(u, svcToID, idKind)
			g.addResourceEdges(u, refToID, idKind)
		}
	}
	return g
}

// ensureNode adds a node for a referenced unit if it is not already present, so
// a target in another quadlet is drawn even when the focus is narrower.
func (g *depGraph) ensureNode(id, kind string) {
	if _, ok := g.nodes[id]; !ok {
		g.nodes[id] = graphNode{ID: id, Kind: kind, External: kind == "external"}
	}
}

// addUnitEdges adds an edge for each [Unit] dependency directive. A target that
// names a managed unit resolves to it; an unmanaged one (e.g.
// network-online.target) becomes a distinct external node so the declared
// dependency is still visible.
func (g *depGraph) addUnitEdges(u eval.UnitRecord, svcToID, idKind map[string]string) {
	for _, directive := range unitDirectives {
		for _, target := range nestedList(u.Data, "Unit", directive) {
			to := target
			if id, ok := svcToID[target]; ok {
				to = id
				g.ensureNode(id, idKind[id])
			} else {
				g.ensureNode(target, "external")
			}
			g.edges = append(g.edges, graphEdge{From: u.Filename, To: to, Rel: directive})
		}
	}
}

// addResourceEdges adds edges for a container's image (when it consumes a
// managed .build/.image), its pod, its networks, and its volumes. Only managed
// units become edges; raw registry images, host paths, and network modes (host,
// container:...) are not graph nodes.
func (g *depGraph) addResourceEdges(u eval.UnitRecord, refToID, idKind map[string]string) {
	link := func(ref, rel string) {
		if id, ok := refToID[ref]; ok {
			g.ensureNode(id, idKind[id])
			g.edges = append(g.edges, graphEdge{From: u.Filename, To: id, Rel: rel})
		}
	}
	if img := topStr(u.Data, "imageString"); img != "" {
		link(img, "image")
	}
	if pod := topStr(u.Data, "podString"); pod != "" {
		link(pod, "pod")
	}
	for _, n := range topList(u.Data, "networkStrings") {
		link(firstField(n), "network")
	}
	for _, v := range topList(u.Data, "volumeStrings") {
		link(firstField(v), "volume")
	}
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
	return edges
}
