package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// relOrdering is true for [Unit] ordering directives, drawn as dashed edges.
func relOrdering(rel string) bool { return rel == "After" || rel == "Before" }

// relResource is true for the resource-coupling edges (image/pod/network/volume).
func relResource(rel string) bool {
	switch rel {
	case "image", "pod", "network", "volume":
		return true
	}
	return false
}

// labelRels chooses what a merged edge's label shows: the [Unit] directives
// when any are present (the resource kind is already conveyed by the target
// node's shape, so "After+Requires+volume" collapses to "After+Requires"),
// otherwise the resource relationship itself for a bare resource edge.
func labelRels(rels []string) []string {
	var directives []string
	for _, r := range rels {
		if !relResource(r) {
			directives = append(directives, r)
		}
	}
	if len(directives) > 0 {
		return directives
	}
	return rels
}

// edgeCategory picks a merged edge's style class: any requirement/failure
// relationship dominates (solid), else resource coupling (dotted), else an
// ordering-only edge (dashed).
func edgeCategory(rels []string) string {
	resource := false
	for _, r := range rels {
		switch {
		case relResource(r):
			resource = true
		case relOrdering(r):
			// ordering-only unless a resource/requirement rel is also present
		default:
			return "requirement"
		}
	}
	if resource {
		return "resource"
	}
	return "ordering"
}

// --- dot (Graphviz) ---

func dotShape(kind string) string {
	switch kind {
	case "container":
		return "box"
	case "pod":
		return "box3d"
	case "volume":
		return "cylinder"
	case "network":
		return "ellipse"
	case "build", "image":
		return "note"
	case "kube":
		return "folder"
	case "artifact":
		return "tab"
	default: // external
		return "box"
	}
}

// dotNodeStmt renders a single node statement (without indentation).
func dotNodeStmt(n graphNode) string {
	attrs := fmt.Sprintf("shape=%s", dotShape(n.Kind))
	if n.External {
		attrs += ` style="rounded,dashed" fontcolor="gray40" color="gray60"`
	}
	return fmt.Sprintf("%q [%s];", n.ID, attrs)
}

func writeDot(w io.Writer, g depGraph, grouped bool) {
	fmt.Fprintln(w, "digraph creidhne {")
	fmt.Fprintln(w, "  rankdir=LR;")
	fmt.Fprintln(w, "  compound=true;")
	fmt.Fprintln(w, `  node [fontname="sans-serif" style="rounded,filled" fillcolor="white"];`)
	fmt.Fprintln(w, `  edge [fontname="sans-serif" fontsize=10];`)
	if grouped {
		order, byQuadlet, ungrouped := g.quadletGroups()
		for i, q := range order {
			// The name must start with "cluster" for graphviz to box it.
			fmt.Fprintf(w, "  subgraph cluster_%d {\n", i)
			fmt.Fprintf(w, "    label=%q;\n", q)
			fmt.Fprintln(w, `    style="rounded"; color="gray70"; labeljust="l"; fontname="sans-serif";`)
			for _, id := range byQuadlet[q] {
				fmt.Fprintf(w, "    %s\n", dotNodeStmt(g.nodes[id]))
			}
			fmt.Fprintln(w, "  }")
		}
		if len(ungrouped) > 0 {
			fmt.Fprintln(w, "  subgraph cluster_external {")
			fmt.Fprintln(w, `    label="external";`)
			fmt.Fprintln(w, `    style="rounded,dashed"; color="gray70"; fontcolor="gray40"; labeljust="l"; fontname="sans-serif";`)
			for _, id := range ungrouped {
				fmt.Fprintf(w, "    %s\n", dotNodeStmt(g.nodes[id]))
			}
			fmt.Fprintln(w, "  }")
		}
	} else {
		for _, id := range g.sortedNodeIDs() {
			fmt.Fprintf(w, "  %s\n", dotNodeStmt(g.nodes[id]))
		}
	}
	for _, e := range g.mergedEdges() {
		var style string
		switch edgeCategory(e.Rels) {
		case "ordering":
			style = ` style=dashed color="gray50"`
		case "resource":
			style = ` style=dotted color="#3572A5"`
		}
		fmt.Fprintf(w, "  %q -> %q [label=%q%s];\n", e.From, e.To, strings.Join(labelRels(e.Rels), "+"), style)
	}
	fmt.Fprintln(w, "}")
}

// --- mermaid ---

// mermaidLabel makes a label safe to place inside Mermaid's ["..."] quoting.
// Mermaid ignores backslash escapes there, so a literal double quote would close
// the string and break the whole render. Managed unit filenames are validated to
// contain none, but an external node's id is a raw [Unit] target that a loose
// #ServiceName regex can admit with a quote, so neutralize it defensively.
func mermaidLabel(s string) string {
	return strings.ReplaceAll(s, `"`, "'")
}

// mermaidNode declares a node with the shape for its kind: id is the synthetic
// mermaid id, label is the unit filename shown to the reader.
func mermaidNode(id, label, kind string) string {
	label = mermaidLabel(label)
	switch kind {
	case "pod":
		return fmt.Sprintf("%s[[%q]]", id, label)
	case "volume":
		return fmt.Sprintf("%s[(%q)]", id, label)
	case "network":
		// Hexagon, not a circle: mermaid sizes circles to the label width, so a
		// long node name blows the circle up.
		return fmt.Sprintf("%s{{%q}}", id, label)
	case "build", "image":
		// Parallelogram, distinct from the network hexagon.
		return fmt.Sprintf("%s[/%q/]", id, label)
	default: // container/kube/artifact/external
		return fmt.Sprintf("%s[%q]", id, label)
	}
}

func writeMermaid(w io.Writer, g depGraph, grouped bool) {
	// Mermaid node ids can't contain dots/dashes, so assign stable synthetic ids
	// (n0, n1, ...) and carry the filename as the label.
	ids := g.sortedNodeIDs()
	mid := make(map[string]string, len(ids))
	for i, id := range ids {
		mid[id] = fmt.Sprintf("n%d", i)
	}
	node := func(id string) string { return mermaidNode(mid[id], id, g.nodes[id].Kind) }

	fmt.Fprintln(w, "graph LR")
	if grouped {
		order, byQuadlet, ungrouped := g.quadletGroups()
		for i, q := range order {
			fmt.Fprintf(w, "  subgraph sg%d[%q]\n", i, mermaidLabel(q))
			for _, id := range byQuadlet[q] {
				fmt.Fprintf(w, "    %s\n", node(id))
			}
			fmt.Fprintln(w, "  end")
		}
		if len(ungrouped) > 0 {
			fmt.Fprintln(w, `  subgraph sgExt["external"]`)
			for _, id := range ungrouped {
				fmt.Fprintf(w, "    %s\n", node(id))
			}
			fmt.Fprintln(w, "  end")
		}
	} else {
		for _, id := range ids {
			fmt.Fprintf(w, "  %s\n", node(id))
		}
	}
	external := false
	for _, id := range ids {
		if g.nodes[id].External {
			fmt.Fprintf(w, "  class %s external;\n", mid[id])
			external = true
		}
	}
	for _, e := range g.mergedEdges() {
		arrow := "-->"
		if edgeCategory(e.Rels) == "ordering" {
			arrow = "-.->"
		}
		fmt.Fprintf(w, "  %s %s|%s| %s\n", mid[e.From], arrow, strings.Join(labelRels(e.Rels), "+"), mid[e.To])
	}
	if external {
		fmt.Fprintln(w, "  classDef external stroke-dasharray:4,color:gray;")
	}
}

// --- json ---

func writeGraphJSON(w io.Writer, g depGraph) error {
	out := struct {
		Nodes []graphNode `json:"nodes"`
		Edges []graphEdge `json:"edges"`
	}{Edges: g.sortedEdges()}
	for _, id := range g.sortedNodeIDs() {
		out.Nodes = append(out.Nodes, g.nodes[id])
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
