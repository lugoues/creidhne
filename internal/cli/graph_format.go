package cli

import (
	"encoding/json"
	"fmt"
	"io"
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

func writeDot(w io.Writer, g depGraph) {
	fmt.Fprintln(w, "digraph creidhne {")
	fmt.Fprintln(w, "  rankdir=LR;")
	fmt.Fprintln(w, `  node [fontname="sans-serif" style="rounded,filled" fillcolor="white"];`)
	fmt.Fprintln(w, `  edge [fontname="sans-serif" fontsize=10];`)
	for _, id := range g.sortedNodeIDs() {
		n := g.nodes[id]
		attrs := fmt.Sprintf("shape=%s", dotShape(n.Kind))
		if n.External {
			attrs += ` style="rounded,dashed" fontcolor="gray40" color="gray60"`
		}
		fmt.Fprintf(w, "  %q [%s];\n", n.ID, attrs)
	}
	for _, e := range g.sortedEdges() {
		var style string
		switch {
		case relOrdering(e.Rel):
			style = ` style=dashed color="gray50"`
		case relResource(e.Rel):
			style = ` style=dotted color="#3572A5"`
		}
		fmt.Fprintf(w, "  %q -> %q [label=%q%s];\n", e.From, e.To, e.Rel, style)
	}
	fmt.Fprintln(w, "}")
}

// --- mermaid ---

// mermaidNode declares a node with the shape for its kind: id is the synthetic
// mermaid id, label is the unit filename shown to the reader.
func mermaidNode(id, label, kind string) string {
	switch kind {
	case "pod":
		return fmt.Sprintf("%s[[%q]]", id, label)
	case "volume":
		return fmt.Sprintf("%s[(%q)]", id, label)
	case "network":
		return fmt.Sprintf("%s((%q))", id, label)
	case "build", "image":
		return fmt.Sprintf("%s{{%q}}", id, label)
	default: // container/kube/artifact/external
		return fmt.Sprintf("%s[%q]", id, label)
	}
}

func writeMermaid(w io.Writer, g depGraph) {
	// Mermaid node ids can't contain dots/dashes, so assign stable synthetic ids
	// (n0, n1, ...) and carry the filename as the label.
	ids := g.sortedNodeIDs()
	mid := make(map[string]string, len(ids))
	for i, id := range ids {
		mid[id] = fmt.Sprintf("n%d", i)
	}

	fmt.Fprintln(w, "graph LR")
	for _, id := range ids {
		fmt.Fprintf(w, "  %s\n", mermaidNode(mid[id], id, g.nodes[id].Kind))
	}
	external := false
	for _, id := range ids {
		if g.nodes[id].External {
			fmt.Fprintf(w, "  class %s external;\n", mid[id])
			external = true
		}
	}
	for _, e := range g.sortedEdges() {
		arrow := "-->"
		if relOrdering(e.Rel) {
			arrow = "-.->"
		}
		fmt.Fprintf(w, "  %s %s|%s| %s\n", mid[e.From], arrow, e.Rel, mid[e.To])
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
