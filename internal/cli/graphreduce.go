package cli

import (
	"sort"
	"strings"
)

// reducedGraph is the drawio renderer's view of a depGraph after the
// noise-reduction rules. The rules only hide, never delete: a folded build
// survives in its consumer's label, an orphan in the quarantine box.
type reducedGraph struct {
	g depGraph
	// folded maps a container id to the build node id folded into its label:
	// the container's own build (same quadlet, same stem), whose edge and
	// node carry zero information beyond "this container is built".
	folded map[string]string
	// hiddenBuilds are folded builds no other edge touches; they render only
	// as their consumer's label line, not as nodes.
	hiddenBuilds map[string]bool
	// orphans are degree-0 node ids (over the unreduced edge set), rendered
	// in a separate quarantine box instead of a stack.
	orphans []string
	// visible are the nodes drawn as their own vertex: everything minus
	// hidden folded builds minus orphans.
	visible map[string]bool
	counts  reduceCounts
}

// reduceCounts is the regression surface for the reduction rules.
type reduceCounts struct {
	Units     int // all nodes
	Relations int // deduped edges, before reduction
	Cross     int // kept edges whose endpoints live in different stacks
	Folded    int // build nodes hidden into their consumer's label
	Orphans   int // degree-0 nodes
}

// stemOf strips the unit-kind extension: "app.container" -> "app". External
// ids (network-online.target, ...) keep enough of themselves to stay readable.
func stemOf(id string) string {
	if i := strings.LastIndex(id, "."); i > 0 {
		return id[:i]
	}
	return id
}

// stackOf groups a node for rendering: its owning quadlet, or "external" for
// unmanaged targets referenced by [Unit] directives.
func stackOf(n graphNode) string {
	if n.Quadlet == "" {
		return "external"
	}
	return n.Quadlet
}

// crossEdge is true when an edge's endpoints live in different stacks — the
// signal the whole drawio redesign exists to surface.
func (r reducedGraph) crossEdge(from, to string) bool {
	return stackOf(r.g.nodes[from]) != stackOf(r.g.nodes[to])
}

// reduceGraph applies the reduction rules, in order:
//
//	R1 fold a container's own build (image edge to a same-quadlet build with
//	   the same stem) into the container's label. A build whose name differs
//	   from its consumer is real information and stays a node, as does one
//	   still touched by any other edge.
//	R2 cross-boundary marking (via crossEdge, computed on demand).
//	R3 quarantine degree-0 nodes (over the unreduced edges) — never drop.
//	R4 the visible set is what remains.
func reduceGraph(g depGraph) reducedGraph {
	r := reducedGraph{
		g:            g,
		folded:       map[string]string{},
		hiddenBuilds: map[string]bool{},
		visible:      map[string]bool{},
	}
	edges := g.sortedEdges()

	// R1: fold candidates, then keep any build that some non-folded edge
	// still touches (a second consumer, a hand-written [Unit] dependency on
	// the build service): the extra relationship is exactly what hiding the
	// node would lose.
	for _, e := range edges {
		to := g.nodes[e.To]
		from := g.nodes[e.From]
		if e.Rel == "image" && to.Kind == "build" &&
			to.Quadlet == from.Quadlet && stemOf(e.From) == stemOf(e.To) {
			r.folded[e.From] = e.To
			r.hiddenBuilds[e.To] = true
		}
	}
	for _, e := range edges {
		if r.foldedEdge(e) {
			continue
		}
		delete(r.hiddenBuilds, e.To)
		delete(r.hiddenBuilds, e.From)
	}

	// R3: degree over the unreduced set.
	deg := map[string]int{}
	for _, e := range edges {
		deg[e.From]++
		deg[e.To]++
	}
	for _, id := range g.sortedNodeIDs() {
		if deg[id] == 0 {
			r.orphans = append(r.orphans, id)
		}
	}
	sort.Strings(r.orphans)

	// R4.
	orphan := map[string]bool{}
	for _, id := range r.orphans {
		orphan[id] = true
	}
	for id := range g.nodes {
		if !r.hiddenBuilds[id] && !orphan[id] {
			r.visible[id] = true
		}
	}

	cross := 0
	for _, e := range r.keptEdges() {
		if r.crossEdge(e.From, e.To) {
			cross++
		}
	}
	r.counts = reduceCounts{
		Units:     len(g.nodes),
		Relations: len(edges),
		Cross:     cross,
		Folded:    len(r.hiddenBuilds),
		Orphans:   len(r.orphans),
	}
	return r
}

// foldedEdge is true for an image edge folded into its consumer's label; the
// edge is dropped only when the build node itself is hidden — a build kept
// visible by other relationships keeps its consumer edge too.
func (r reducedGraph) foldedEdge(e graphEdge) bool {
	return e.Rel == "image" && r.folded[e.From] == e.To && r.hiddenBuilds[e.To]
}

// keptEdges returns the deduped edges minus the folded build edges: the
// renderer's working set.
func (r reducedGraph) keptEdges() []graphEdge {
	var out []graphEdge
	for _, e := range r.g.sortedEdges() {
		if r.foldedEdge(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}
