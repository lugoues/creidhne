package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// The drawio renderer emits a five-page uncompressed .drawio file:
//
//	1. Overview      — stack-level boxes, cross-boundary resource relations only
//	2. Networks      — per-stack: containers left, the networks they join right
//	3. Storage       — per-stack: containers left, volumes right, pod membership
//	4. Full detail   — every visible unit and resource relation, plus the
//	                   orphan quarantine box
//	5. Dependencies  — the declared [Unit] dependency graph as a layered tree
//
// Geometry is computed here (shelf packing, column per kind); no layout
// library, no drawio auto-layout: coordinates on disk keep the file usable
// in any viewer and diffs meaningful.

// drawio node/edge style tables — the single source of truth; never inline
// style strings at call sites. Non-rectangular shapes carry their matching
// perimeter= so edges attach to the outline, not the bounding box.
const drawioFont = "fontFamily=Helvetica;fontSize=12;html=1;whiteSpace=wrap;"

var drawioNodeStyle = map[string]string{
	"container": "rounded=1;arcSize=12;fillColor=#dae8fc;strokeColor=#6c8ebf;fontColor=#1a1a1a;" + drawioFont,
	"network":   "shape=hexagon;perimeter=hexagonPerimeter2;fillColor=#d5e8d4;strokeColor=#82b366;fontColor=#1a1a1a;" + drawioFont,
	"volume":    "shape=cylinder3;boundedLbl=1;backgroundOutline=1;size=8;fillColor=#e1d5e7;strokeColor=#9673a6;fontColor=#1a1a1a;" + drawioFont,
	"build":     "shape=parallelogram;perimeter=parallelogramPerimeter;fixedSize=1;size=14;fillColor=#ffe6cc;strokeColor=#d79b00;fontColor=#1a1a1a;" + drawioFont,
	"image":     "shape=parallelogram;perimeter=parallelogramPerimeter;fixedSize=1;size=14;fillColor=#fff2cc;strokeColor=#d6b656;fontColor=#1a1a1a;" + drawioFont,
	"artifact":  "shape=parallelogram;perimeter=parallelogramPerimeter;fixedSize=1;size=14;fillColor=#fff2cc;strokeColor=#d6b656;fontColor=#1a1a1a;" + drawioFont,
	"pod":       "shape=process;size=0.08;fillColor=#b1ddf0;strokeColor=#10739e;fontColor=#1a1a1a;" + drawioFont,
	"kube":      "rounded=1;arcSize=12;fillColor=#fff2cc;strokeColor=#d6b656;fontColor=#1a1a1a;" + drawioFont,
	"external":  "rounded=1;arcSize=12;dashed=1;fillColor=#f5f5f5;strokeColor=#999999;fontColor=#666666;" + drawioFont,
}

const (
	drawioStackStyle = "rounded=1;arcSize=4;container=1;collapsible=0;fillColor=#fafafa;strokeColor=#b3b3b3;" +
		"verticalAlign=top;align=left;spacingLeft=10;spacingTop=4;fontSize=15;fontStyle=1;" +
		"fontColor=#444444;html=1;whiteSpace=wrap;fontFamily=Helvetica;"
	drawioExternalStackStyle = "rounded=1;arcSize=4;container=1;collapsible=0;dashed=1;fillColor=#f5f5f5;strokeColor=#999999;" +
		"verticalAlign=top;align=left;spacingLeft=10;spacingTop=4;fontSize=15;fontStyle=1;" +
		"fontColor=#666666;html=1;whiteSpace=wrap;fontFamily=Helvetica;"
	drawioHubStyle = "rounded=1;arcSize=8;fillColor=#eef3fb;strokeColor=#6c8ebf;verticalAlign=top;align=center;" +
		"spacingTop=6;fontSize=14;fontStyle=1;fontColor=#1a1a1a;html=1;whiteSpace=wrap;fontFamily=Helvetica;"
	drawioTitleStyle = "text;html=1;align=left;verticalAlign=middle;fontSize=22;fontStyle=1;fontColor=#111111;fontFamily=Helvetica;"
	drawioSubStyle   = "text;html=1;align=left;verticalAlign=top;fontSize=12;fontColor=#555555;fontFamily=Helvetica;"
)

const drawioEdgeBase = "edgeStyle=orthogonalEdgeStyle;rounded=1;jettySize=auto;html=1;endArrow=block;endFill=1;" +
	"endSize=6;fontSize=10;fontFamily=Helvetica;labelBackgroundColor=#ffffff;"

var drawioEdgeStyle = map[string]string{
	"network": drawioEdgeBase + "strokeColor=#82b366;",
	"volume":  drawioEdgeBase + "strokeColor=#9673a6;",
	"pod":     drawioEdgeBase + "strokeColor=#10739e;",
	"image":   drawioEdgeBase + "strokeColor=#d79b00;dashed=1;",
}

const (
	// Cross-boundary overrides the type colour: the one visual rule that
	// makes the estate's coupling legible at a glance.
	drawioEdgeCross = drawioEdgeBase + "strokeColor=#b85450;strokeWidth=2;"
	// Deps ([Unit] ordering/requirement) edges on the detail page: grey
	// dotted, deliberately quieter than resource coupling.
	drawioEdgeDeps = drawioEdgeBase + "strokeColor=#999999;dashed=1;dashPattern=1 3;"
)

// Layout constants (page-agnostic).
const (
	dwNodeW, dwNodeH   = 230.0, 46.0
	dwGapX, dwGapY     = 26.0, 30.0
	dwPadX             = 18.0
	dwPadTop, dwPadBot = 34.0, 18.0
	dwShelfMaxW        = 3900.0
	dwShelfGap         = 40.0
	dwMargin           = 40.0
)

// xmlEsc escapes a value for an XML attribute; the HTML the label carries
// (<br/>, <font>) must arrive escaped so drawio re-parses it as HTML.
var xmlEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")

// drawioPage accumulates the mxCell rows of one <diagram>. Ids are prefixed
// per page so copy-paste between pages cannot collide.
type drawioPage struct {
	name   string
	prefix string
	rows   []string
	seq    int
	// extent of top-level cells only; children are parent-relative.
	maxX, maxY float64
}

func (p *drawioPage) nextID() string {
	p.seq++
	return fmt.Sprintf("%s%d", p.prefix, p.seq)
}

// vertex emits a node. Children of a stack box pass the box id as parent and
// use coordinates relative to it.
func (p *drawioPage) vertex(label, style string, x, y, w, h float64, parent string) string {
	id := p.nextID()
	p.rows = append(p.rows, fmt.Sprintf(
		`<mxCell id="%s" value="%s" style="%s" vertex="1" parent="%s"><mxGeometry x="%g" y="%g" width="%g" height="%g" as="geometry"/></mxCell>`,
		id, xmlEsc.Replace(label), style, parent, x, y, w, h))
	if parent == "1" {
		if x+w > p.maxX {
			p.maxX = x + w
		}
		if y+h > p.maxY {
			p.maxY = y + h
		}
	}
	return id
}

// edge emits a connection. Edges always use parent="1": drawio resolves
// endpoints nested inside containers correctly.
func (p *drawioPage) edge(src, dst, style, label string) {
	p.rows = append(p.rows, fmt.Sprintf(
		`<mxCell id="%s" value="%s" style="%s" edge="1" parent="1" source="%s" target="%s"><mxGeometry relative="1" as="geometry"/></mxCell>`,
		p.nextID(), xmlEsc.Replace(label), style, src, dst))
}

// xml renders the whole <diagram>, sizing the page to its content: a fixed
// page size with content beyond it is exactly the opens-looking-empty failure
// this renderer replaces.
func (p *drawioPage) xml(w io.Writer) {
	pw, ph := int(p.maxX)+60, int(p.maxY)+60
	pid := strings.NewReplacer(" ", "_", ".", "").Replace(p.name)
	fmt.Fprintf(w, "  <diagram name=%q id=%q>\n", p.name, pid)
	fmt.Fprintf(w, `    <mxGraphModel dx="1400" dy="900" grid="1" gridSize="10" guides="1" tooltips="1" connect="1" arrows="1" fold="1" page="1" pageScale="1" pageWidth="%d" pageHeight="%d" math="0" shadow="0">`+"\n", pw, ph)
	fmt.Fprint(w, "      <root>\n        <mxCell id=\"0\"/>\n        <mxCell id=\"1\" parent=\"0\"/>\n")
	for _, r := range p.rows {
		fmt.Fprintf(w, "        %s\n", r)
	}
	fmt.Fprint(w, "      </root>\n    </mxGraphModel>\n  </diagram>\n")
}

// nodeLabel renders a unit vertex label: the stem, plus the folded build's
// name as a second line when the build renders nowhere else.
func nodeLabel(r reducedGraph, id string) string {
	label := stemOf(id)
	if b, ok := r.folded[id]; ok && r.hiddenBuilds[b] {
		label += fmt.Sprintf("<br/><font style='font-size:9px;color:#a06000'>&#9670; %s</font>", b)
	}
	return label
}

func nodeStyle(n graphNode) string {
	if n.External {
		return drawioNodeStyle["external"]
	}
	if s, ok := drawioNodeStyle[n.Kind]; ok {
		return s
	}
	return drawioNodeStyle["external"]
}

// shelfLayout places the stacks touched by the selected edges: one box per
// stack, a column per kind (kindOrder), boxes shelf-packed left-to-right and
// wrapped at dwShelfMaxW. Alphabetical within a column: this is a reference
// diagram people search by eye, not an optimization target. Returns the
// vertex ids and the cursor after the last box, so callers can append more
// boxes (the orphan quarantine) on the same shelf.
func (p *drawioPage) shelfLayout(r reducedGraph, kindOrder []string, keep func(graphEdge) bool, y0 float64) (ids map[string]string, x, y, shelfH float64) {
	inOrder := map[string]bool{}
	for _, k := range kindOrder {
		inOrder[k] = true
	}
	kindOf := func(id string) string {
		n := r.g.nodes[id]
		if n.External {
			return "external"
		}
		return n.Kind
	}
	var selected []graphEdge
	used := map[string]bool{}
	for _, e := range r.keptEdges() {
		if !keep(e) || !r.visible[e.From] || !r.visible[e.To] {
			continue
		}
		if !inOrder[kindOf(e.From)] || !inOrder[kindOf(e.To)] {
			continue
		}
		selected = append(selected, e)
		used[e.From], used[e.To] = true, true
	}

	byStack := map[string]map[string][]string{}
	stackSet := map[string]bool{}
	for id := range used {
		s := stackOf(r.g.nodes[id])
		stackSet[s] = true
		if byStack[s] == nil {
			byStack[s] = map[string][]string{}
		}
		k := kindOf(id)
		byStack[s][k] = append(byStack[s][k], id)
	}
	stacks := make([]string, 0, len(stackSet))
	for s := range stackSet {
		stacks = append(stacks, s)
	}
	sort.Strings(stacks)

	ids = map[string]string{}
	x, y, shelfH = dwMargin, y0, 0
	for _, stack := range stacks {
		type column struct {
			kind string
			ids  []string
		}
		var cols []column
		rows := 0
		for _, k := range kindOrder {
			col := byStack[stack][k]
			if len(col) == 0 {
				continue
			}
			sort.Strings(col)
			cols = append(cols, column{k, col})
			if len(col) > rows {
				rows = len(col)
			}
		}
		bw := dwPadX*2 + float64(len(cols))*dwNodeW + float64(len(cols)-1)*dwGapX
		bh := dwPadTop + dwPadBot + float64(rows)*dwNodeH + float64(rows-1)*dwGapY
		if x+bw > dwShelfMaxW {
			x, y, shelfH = dwMargin, y+shelfH+dwShelfGap, 0
		}
		style := drawioStackStyle
		if stack == "external" {
			style = drawioExternalStackStyle
		}
		box := p.vertex(stack, style, x, y, bw, bh, "1")
		for ci, col := range cols {
			for ri, id := range col.ids {
				ids[id] = p.vertex(nodeLabel(r, id), nodeStyle(r.g.nodes[id]),
					dwPadX+float64(ci)*(dwNodeW+dwGapX), dwPadTop+float64(ri)*(dwNodeH+dwGapY),
					dwNodeW, dwNodeH, box)
			}
		}
		if bh > shelfH {
			shelfH = bh
		}
		x += bw + dwShelfGap
	}

	for _, e := range selected {
		style, ok := drawioEdgeStyle[e.Rel]
		if !ok {
			style = drawioEdgeDeps
		}
		if r.crossEdge(e.From, e.To) {
			style = drawioEdgeCross
		}
		p.edge(ids[e.From], ids[e.To], style, "")
	}
	return ids, x, y, shelfH
}

func (p *drawioPage) heading(title, sub string) {
	p.vertex(title, drawioTitleStyle, dwMargin, 24, 900, 34, "1")
	p.vertex(sub, drawioSubStyle, dwMargin, 62, 1500, 40, "1")
}

// pageOverview draws stack-level boxes and only the resource relations that
// cross a stack boundary, deduplicated per (from-stack, to-stack, type). The
// stack with the most cross-boundary network relations is the ingress hub.
func pageOverview(r reducedGraph) *drawioPage {
	p := &drawioPage{name: "1. Overview", prefix: "ov"}

	type stackEdge struct{ from, to, rel, label string }
	var edges []stackEdge
	seen := map[[3]string]bool{}
	netCross := map[string]int{}
	involved := map[string]bool{}
	for _, e := range r.keptEdges() {
		if !relResource(e.Rel) || !r.crossEdge(e.From, e.To) {
			continue
		}
		a, b := stackOf(r.g.nodes[e.From]), stackOf(r.g.nodes[e.To])
		involved[a], involved[b] = true, true
		if e.Rel == "network" {
			netCross[a]++
		}
		key := [3]string{a, b, e.Rel}
		if seen[key] {
			continue
		}
		seen[key] = true
		edges = append(edges, stackEdge{a, b, e.Rel, stemOf(e.To)})
	}

	p.heading("Overview",
		fmt.Sprintf("%d units, %d relations. Drawn here: the %d cross-stack resource links (deduplicated per stack pair; %d relations cross a boundary in total, [Unit] ordering included — see page 5).",
			r.counts.Units, r.counts.Relations, len(edges), r.counts.Cross))

	hub := ""
	for s, n := range netCross {
		if hub == "" || n > netCross[hub] || (n == netCross[hub] && s < hub) {
			hub = s
		}
	}
	var spokes []string
	for s := range involved {
		if s != hub {
			spokes = append(spokes, s)
		}
	}
	sort.Strings(spokes)

	boxes := map[string]string{}
	if hub != "" {
		boxes[hub] = p.vertex(
			fmt.Sprintf("<b>%s</b><br/><font style='font-size:10px;color:#666'>ingress hub</font>", hub),
			drawioHubStyle, 700, 560, 300, 80, "1")
	}
	const perRow = 4
	for i, s := range spokes {
		col, row := i%perRow, i/perRow
		boxes[s] = p.vertex("<b>"+s+"</b>", drawioHubStyle,
			60+float64(col)*340, 820+float64(row)*110, 260, 70, "1")
	}

	for _, e := range edges {
		style := drawioEdgeStyle[e.rel] + "strokeWidth=2;"
		if e.rel == "network" {
			style = drawioEdgeCross
		}
		p.edge(boxes[e.from], boxes[e.to], style, e.label)
	}
	return p
}

func pageNetworks(r reducedGraph) *drawioPage {
	p := &drawioPage{name: "2. Networks", prefix: "nw"}
	n := 0
	for _, e := range r.keptEdges() {
		if e.Rel == "network" {
			n++
		}
	}
	p.heading("Network topology",
		fmt.Sprintf("%d network relations. Containers on the left of each stack, the networks they join on the right. Red edges cross a stack boundary. Builds are folded into their container.", n))
	p.shelfLayout(r, []string{"container", "pod", "kube", "network"},
		func(e graphEdge) bool { return e.Rel == "network" }, 120)
	return p
}

func pageStorage(r reducedGraph) *drawioPage {
	p := &drawioPage{name: "3. Storage", prefix: "st"}
	nv, np := 0, 0
	for _, e := range r.keptEdges() {
		switch e.Rel {
		case "volume":
			nv++
		case "pod":
			np++
		}
	}
	p.heading("Volumes and pods",
		fmt.Sprintf("%d volume relations and %d pod memberships. Red edges are volumes shared across stacks.", nv, np))
	p.shelfLayout(r, []string{"container", "pod", "kube", "volume"},
		func(e graphEdge) bool { return e.Rel == "volume" || e.Rel == "pod" }, 120)
	return p
}

// pageDetail draws everything visible, resource relations only — [Unit]
// dependencies live on the dedicated dependency page. Orphans land in a
// dashed quarantine box.
func pageDetail(r reducedGraph) *drawioPage {
	p := &drawioPage{name: "4. Full detail", prefix: "fd"}
	p.heading("Full detail",
		fmt.Sprintf("Every visible unit and resource relation. %d build units are folded into their container label. [Unit] ordering/requirement dependencies are on page 5.", r.counts.Folded))

	kinds := []string{"pod", "container", "kube", "build", "image", "artifact", "network", "volume", "external"}
	ids, x, y, shelfH := p.shelfLayout(r, kinds, func(e graphEdge) bool { return relResource(e.Rel) }, 120)

	// Units whose only relationships are [Unit] dependencies have no resource
	// edge to place them, but this page promises every visible unit: place
	// them in trailing per-stack boxes (their edges render on page 5).
	missing := map[string][]string{}
	for id := range r.visible {
		if _, placed := ids[id]; placed {
			continue
		}
		s := stackOf(r.g.nodes[id])
		missing[s] = append(missing[s], id)
	}
	stacks := make([]string, 0, len(missing))
	for s := range missing {
		stacks = append(stacks, s)
	}
	sort.Strings(stacks)
	for _, stack := range stacks {
		col := missing[stack]
		sort.Strings(col)
		rows := len(col)
		bw := dwPadX*2 + dwNodeW
		bh := dwPadTop + dwPadBot + float64(rows)*dwNodeH + float64(rows-1)*dwGapY
		if x+bw > dwShelfMaxW {
			x, y, shelfH = dwMargin, y+shelfH+dwShelfGap, 0
		}
		style := drawioStackStyle
		if stack == "external" {
			style = drawioExternalStackStyle
		}
		box := p.vertex(stack, style, x, y, bw, bh, "1")
		for ri, id := range col {
			ids[id] = p.vertex(nodeLabel(r, id), nodeStyle(r.g.nodes[id]),
				dwPadX, dwPadTop+float64(ri)*(dwNodeH+dwGapY), dwNodeW, dwNodeH, box)
		}
		if bh > shelfH {
			shelfH = bh
		}
		x += bw + dwShelfGap
	}

	// Orphan quarantine: parsed but wired to nothing. Real signal, never
	// silently dropped.
	if len(r.orphans) > 0 {
		rows := len(r.orphans)
		bw := dwPadX*2 + dwNodeW
		bh := dwPadTop + dwPadBot + float64(rows)*dwNodeH + float64(rows-1)*dwGapY
		if x+bw > dwShelfMaxW {
			x, y = dwMargin, y+shelfH+dwShelfGap
		}
		box := p.vertex("unreferenced units", drawioExternalStackStyle, x, y, bw, bh, "1")
		for ri, id := range r.orphans {
			p.vertex(id, drawioNodeStyle["external"],
				dwPadX, dwPadTop+float64(ri)*(dwNodeH+dwGapY), dwNodeW, dwNodeH, box)
		}
	}
	return p
}

// pageDeps renders the declared [Unit] dependency graph as a layered tree:
// foundations (units nothing here waits on) in the top band, each unit one
// band below its deepest dependency (longest-path layering), arrows pointing
// from a dependent up to what it awaits. Every declared directive renders —
// this page is about ordering, so nothing is suppressed as redundant.
// Directives per node pair merge into one labeled edge; cross-stack
// dependencies keep the red override.
func pageDeps(r reducedGraph) *drawioPage {
	p := &drawioPage{name: "5. Dependencies", prefix: "dp"}

	// Edges are normalized so from = the unit that waits, to = what it waits
	// on. Most directives already declare that direction (After=, Requires=,
	// ...); the ones systemd defines the other way around are flipped here,
	// or the tree would draw startup order upside down for them: Before=X
	// makes X wait on the declarer, and OnFailure=/OnSuccess=X start X from
	// the declarer's outcome. Labels keep the declared directive name.
	reversed := map[string]bool{"Before": true, "OnFailure": true, "OnSuccess": true}
	type pair struct{ from, to string }
	depRels := map[pair][]string{}
	nodeSet := map[string]bool{}
	for _, e := range r.keptEdges() {
		if relResource(e.Rel) || !r.visible[e.From] || !r.visible[e.To] {
			continue
		}
		k := pair{e.From, e.To}
		if reversed[e.Rel] {
			k = pair{e.To, e.From}
		}
		depRels[k] = append(depRels[k], e.Rel)
		nodeSet[e.From], nodeSet[e.To] = true, true
	}
	p.heading("Dependency tree",
		fmt.Sprintf("Declared [Unit] dependencies (%d edges between %d units), foundations on top. Quadlet's implicit wiring from resource references is not drawn — see pages 2-4 for those.",
			len(depRels), len(nodeSet)))
	if len(depRels) == 0 {
		return p
	}

	// Longest-path layering over the declared edge direction, iteration
	// bounded so a dependency cycle cannot hang the renderer (cyclic units
	// settle wherever the last pass left them).
	pairs := make([]pair, 0, len(depRels))
	for k := range depRels {
		pairs = append(pairs, k)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].from != pairs[j].from {
			return pairs[i].from < pairs[j].from
		}
		return pairs[i].to < pairs[j].to
	})
	layer := map[string]int{}
	for iter, changed := 0, true; changed && iter <= len(nodeSet); iter++ {
		changed = false
		for _, k := range pairs {
			if l := layer[k.to] + 1; l > layer[k.from] {
				layer[k.from] = l
				changed = true
			}
		}
	}

	byLayer := map[int][]string{}
	maxLayer := 0
	for id := range nodeSet {
		l := layer[id]
		byLayer[l] = append(byLayer[l], id)
		if l > maxLayer {
			maxLayer = l
		}
	}

	// Bands top-down, nodes wrapped at the page width; sorted by stack then
	// name so a stack's units cluster within their band.
	ids := map[string]string{}
	y := 120.0
	for l := 0; l <= maxLayer; l++ {
		band := byLayer[l]
		if len(band) == 0 {
			continue
		}
		sort.Slice(band, func(i, j int) bool {
			si, sj := stackOf(r.g.nodes[band[i]]), stackOf(r.g.nodes[band[j]])
			if si != sj {
				return si < sj
			}
			return band[i] < band[j]
		})
		x, rowY := dwMargin, y
		for _, id := range band {
			if x+dwNodeW > dwShelfMaxW {
				x, rowY = dwMargin, rowY+dwNodeH+dwGapY
			}
			// Full filename, not the stem: this page mixes kinds outside
			// their columns, and a container and its network share a stem.
			label := id
			if n := r.g.nodes[id]; !n.External {
				label += fmt.Sprintf("<br/><font style='font-size:9px;color:#888'>%s</font>", stackOf(n))
			}
			ids[id] = p.vertex(label, nodeStyle(r.g.nodes[id]), x, rowY, dwNodeW, dwNodeH, "1")
			x += dwNodeW + dwGapX
		}
		// Wide gap between bands so the layering reads as levels.
		y = rowY + dwNodeH + 3*dwGapY
	}

	for _, k := range pairs {
		rels := depRels[k]
		sort.Strings(rels)
		style := drawioEdgeDeps
		if r.crossEdge(k.from, k.to) {
			style = drawioEdgeCross
		}
		p.edge(ids[k.from], ids[k.to], style, strings.Join(rels, "+"))
	}
	return p
}

// writeDrawio renders the reduced graph as a four-page uncompressed .drawio
// document. Uncompressed on purpose: compressed output is base64+deflate,
// undiffable, and buys nothing at this size. No XML comments anywhere —
// drawio forbids them in generated files.
func writeDrawio(w io.Writer, g depGraph) {
	r := reduceGraph(g)
	fmt.Fprint(w, `<mxfile host="app.diagrams.net" agent="crei-graph" type="device" compressed="false">`+"\n")
	for _, p := range []*drawioPage{pageOverview(r), pageNetworks(r), pageStorage(r), pageDetail(r), pageDeps(r)} {
		p.xml(w)
	}
	fmt.Fprint(w, "</mxfile>\n")
}
