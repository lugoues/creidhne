package cli

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/lugoues/creidhne/internal/eval"
)

// drawioFixture is a small estate exercising every reduction rule: a
// container with its own same-stem build (folds), a build with a mismatched
// name (stays visible), a cross-stack network attachment, [Unit] deps to a
// sibling quadlet and an external target, and a degree-0 orphan.
func drawioFixture() depGraph {
	app := eval.Quadlet{Name: "app", Units: []eval.UnitRecord{
		{Kind: "container", Filename: "app.container", Service: "app.service", Data: map[string]any{
			"imageString":    "app.build",
			"networkStrings": []any{"app.network"},
			"volumeStrings":  []any{"app.volume:/data"},
			"Unit": map[string]any{
				"After":    []any{"network-online.target", "db.service"},
				"Requires": []any{"db.service"},
				// Not subsumed by the Network= reference: must render as a
				// deps edge even though a resource edge covers the same pair.
				"Before": []any{"app-network.service"},
			},
			"Container": map[string]any{},
		}},
		{Kind: "build", Filename: "app.build", Service: "app-build.service", Data: map[string]any{}},
		{Kind: "network", Filename: "app.network", Service: "app-network.service", Data: map[string]any{}},
		{Kind: "volume", Filename: "app.volume", Service: "app-volume.service", Data: map[string]any{}},
	}}
	db := eval.Quadlet{Name: "db", Units: []eval.UnitRecord{
		{Kind: "container", Filename: "db.container", Service: "db.service", Data: map[string]any{
			"imageString":    "db-custom.build",
			"networkStrings": []any{"app.network"},
			"Container":      map[string]any{},
		}},
		{Kind: "build", Filename: "db-custom.build", Service: "db-custom-build.service", Data: map[string]any{}},
	}}
	misc := eval.Quadlet{Name: "misc", Units: []eval.UnitRecord{
		{Kind: "network", Filename: "lonely.network", Service: "lonely-network.service", Data: map[string]any{}},
	}}
	all := []eval.Quadlet{app, db, misc}
	return buildGraph(all, all)
}

// TestReduceGraphCounts pins the reduction rules on the fixture: the same-stem
// build folds, the renamed build survives, the unattached network is
// quarantined, and cross-stack edges (network attachment, deps to another
// quadlet, deps to an external target) are all marked.
func TestReduceGraphCounts(t *testing.T) {
	r := reduceGraph(drawioFixture())
	want := reduceCounts{Units: 8, Relations: 9, Cross: 4, Folded: 1, Orphans: 1}
	if r.counts != want {
		t.Fatalf("counts = %+v, want %+v", r.counts, want)
	}
	if !r.hiddenBuilds["app.build"] {
		t.Error("app.build (same stem, same quadlet) must fold into its container")
	}
	if !r.visible["db-custom.build"] {
		t.Error("db-custom.build (name differs from its consumer) must stay visible")
	}
	if len(r.orphans) != 1 || r.orphans[0] != "lonely.network" {
		t.Errorf("orphans = %v, want [lonely.network]", r.orphans)
	}
	for _, e := range r.keptEdges() {
		if e.Rel == "image" && e.To == "app.build" {
			t.Error("the folded build's image edge must be dropped")
		}
	}
}

// TestReduceKeepsFoldCandidateWithOtherEdges: a build that would fold but is
// still referenced by another edge stays a node (and keeps its image edge).
func TestReduceKeepsFoldCandidateWithOtherEdges(t *testing.T) {
	q := eval.Quadlet{Name: "app", Units: []eval.UnitRecord{
		{Kind: "container", Filename: "app.container", Service: "app.service", Data: map[string]any{
			"imageString": "app.build",
			"Container":   map[string]any{},
		}},
		{Kind: "container", Filename: "sidecar.container", Service: "sidecar.service", Data: map[string]any{
			"imageString": "docker.io/library/img:1",
			"Unit":        map[string]any{"After": []any{"app-build.service"}},
			"Container":   map[string]any{},
		}},
		{Kind: "build", Filename: "app.build", Service: "app-build.service", Data: map[string]any{}},
	}}
	all := []eval.Quadlet{q}
	r := reduceGraph(buildGraph(all, all))
	if r.hiddenBuilds["app.build"] {
		t.Fatal("a build with a hand-written dependency on it must stay visible")
	}
	kept := false
	for _, e := range r.keptEdges() {
		if e.Rel == "image" && e.To == "app.build" {
			kept = true
		}
	}
	if !kept {
		t.Fatal("a visible build keeps its consumer's image edge")
	}
}

// mxCell is the slice of the drawio schema the acceptance tests need.
type mxCell struct {
	ID     string  `xml:"id,attr"`
	Value  string  `xml:"value,attr"`
	Parent string  `xml:"parent,attr"`
	Source string  `xml:"source,attr"`
	Target string  `xml:"target,attr"`
	Vertex string  `xml:"vertex,attr"`
	Edge   string  `xml:"edge,attr"`
	Geo    *mxGeom `xml:"mxGeometry"`
}
type mxGeom struct {
	X float64 `xml:"x,attr"`
	Y float64 `xml:"y,attr"`
	W float64 `xml:"width,attr"`
	H float64 `xml:"height,attr"`
}
type mxModel struct {
	PageWidth  float64  `xml:"pageWidth,attr"`
	PageHeight float64  `xml:"pageHeight,attr"`
	Cells      []mxCell `xml:"root>mxCell"`
}
type mxDiagram struct {
	Name  string  `xml:"name,attr"`
	Model mxModel `xml:"mxGraphModel"`
}
type mxFile struct {
	Compressed string      `xml:"compressed,attr"`
	Diagrams   []mxDiagram `xml:"diagram"`
}

func renderDrawio(t *testing.T, g depGraph) mxFile {
	t.Helper()
	var buf bytes.Buffer
	writeDrawio(&buf, g)
	var f mxFile
	if err := xml.Unmarshal(buf.Bytes(), &f); err != nil {
		t.Fatalf("output is not well-formed XML: %v", err)
	}
	if strings.Contains(buf.String(), "<!--") {
		t.Fatal("drawio output must not contain XML comments")
	}
	if f.Compressed != "false" {
		t.Fatalf("mxfile must declare compressed=%q, got %q", "false", f.Compressed)
	}
	return f
}

// TestDrawioStructure covers the plan's structural acceptance checks on the
// fixture estate: four pages, unique ids, referential integrity, no sibling
// overlap, and content inside the declared page size.
func TestDrawioStructure(t *testing.T) {
	f := renderDrawio(t, drawioFixture())
	if len(f.Diagrams) != 5 {
		t.Fatalf("want 5 pages, got %d", len(f.Diagrams))
	}
	for _, d := range f.Diagrams {
		ids := map[string]bool{}
		for _, c := range d.Model.Cells {
			if ids[c.ID] {
				t.Fatalf("%s: duplicate id %s", d.Name, c.ID)
			}
			ids[c.ID] = true
			if c.Vertex == "1" && c.Edge == "1" {
				t.Fatalf("%s: cell %s is both vertex and edge", d.Name, c.ID)
			}
		}
		byParent := map[string][]mxCell{}
		for _, c := range d.Model.Cells {
			for _, ref := range []string{c.Parent, c.Source, c.Target} {
				if ref != "" && !ids[ref] {
					t.Fatalf("%s: cell %s references missing id %s", d.Name, c.ID, ref)
				}
			}
			if c.Vertex == "1" && c.Geo != nil {
				byParent[c.Parent] = append(byParent[c.Parent], c)
				if c.Parent == "1" {
					if c.Geo.X+c.Geo.W > d.Model.PageWidth || c.Geo.Y+c.Geo.H > d.Model.PageHeight {
						t.Errorf("%s: cell %s (%.0f,%.0f %gx%g) outside page %gx%g",
							d.Name, c.ID, c.Geo.X, c.Geo.Y, c.Geo.W, c.Geo.H, d.Model.PageWidth, d.Model.PageHeight)
					}
				}
			}
		}
		for parent, sibs := range byParent {
			for i := 0; i < len(sibs); i++ {
				for j := i + 1; j < len(sibs); j++ {
					a, b := sibs[i].Geo, sibs[j].Geo
					if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
						t.Errorf("%s: siblings %s and %s overlap under parent %s",
							d.Name, sibs[i].ID, sibs[j].ID, parent)
					}
				}
			}
		}
	}
}

// TestDrawioRoundTrip: no relation is silently lost. Page 4 carries exactly
// the kept resource edges; page 5 carries every declared [Unit] dependency
// pair (merged per pair, nothing suppressed — the page is about ordering).
func TestDrawioRoundTrip(t *testing.T) {
	g := drawioFixture()
	r := reduceGraph(g)
	f := renderDrawio(t, g)

	// Legend swatch samples are point-to-point edges with no endpoints; only
	// connected edges represent relations.
	countEdges := func(d mxDiagram) int {
		n := 0
		for _, c := range d.Model.Cells {
			if c.Edge == "1" && c.Source != "" {
				n++
			}
		}
		return n
	}

	wantResource := 0
	type pair struct{ from, to string }
	depPairs := map[pair]bool{}
	for _, e := range r.keptEdges() {
		if relResource(e.Rel) {
			wantResource++
			continue
		}
		k := pair{e.From, e.To}
		if e.Rel == "Before" || e.Rel == "OnFailure" || e.Rel == "OnSuccess" {
			k = pair{e.To, e.From}
		}
		depPairs[k] = true
	}
	if got := countEdges(f.Diagrams[3]); got != wantResource {
		t.Fatalf("detail page has %d edges, want %d (resource only)", got, wantResource)
	}
	// Deps pairs merge in *normalized* orientation (Before= flips), so count
	// via the same normalization.
	if got := countEdges(f.Diagrams[4]); got != len(depPairs) {
		t.Fatalf("dependency page has %d edges, want %d merged deps pairs", got, len(depPairs))
	}

	// Page 4 promises every visible unit: its in-box vertices (stack and
	// quarantine children, legend excluded) must cover visible + orphans,
	// deps-only units and external targets included.
	legendID := ""
	for _, c := range f.Diagrams[3].Model.Cells {
		if c.Value == "Legend" {
			legendID = c.ID
		}
	}
	units := 0
	for _, c := range f.Diagrams[3].Model.Cells {
		if c.Vertex == "1" && c.Parent != "1" && c.Parent != "" && c.Parent != legendID {
			units++
		}
	}
	if want := len(r.visible) + len(r.orphans); units != want {
		t.Fatalf("detail page places %d units, want %d (visible + orphans)", units, want)
	}
}

// TestDrawioDepsLayering: on the dependency page a dependent sits strictly
// below every unit it waits on (foundations in the top band). Holds for
// acyclic fixtures like this one; a dependency cycle's members share a band
// by design (SCC condensation).
func TestDrawioDepsLayering(t *testing.T) {
	f := renderDrawio(t, drawioFixture())
	deps := f.Diagrams[4]
	yOf := map[string]float64{}
	byID := map[string]mxCell{}
	for _, c := range deps.Model.Cells {
		byID[c.ID] = c
		if c.Vertex == "1" && c.Geo != nil {
			yOf[c.ID] = c.Geo.Y
		}
	}
	checked := 0
	for _, c := range deps.Model.Cells {
		if c.Edge != "1" || c.Source == "" { // legend samples have no endpoints
			continue
		}
		if yOf[c.Source] <= yOf[c.Target] {
			t.Errorf("dependent %s (y=%g) must sit below its dependency %s (y=%g)",
				c.Source, yOf[c.Source], c.Target, yOf[c.Target])
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("expected dependency edges on the fixture")
	}
}

// TestDrawioDepsBeforeDirection: Before=X means X waits on the declarer, so
// the rendered edge (normalized: dependent -> dependency) must run from the
// directive's target back to the declaring unit. The fixture declares
// app.container Before=app-network.service.
func TestDrawioDepsBeforeDirection(t *testing.T) {
	var raw struct {
		Diagrams []struct {
			Cells []struct {
				ID     string `xml:"id,attr"`
				Value  string `xml:"value,attr"`
				Source string `xml:"source,attr"`
				Target string `xml:"target,attr"`
				Edge   string `xml:"edge,attr"`
			} `xml:"mxGraphModel>root>mxCell"`
		} `xml:"diagram"`
	}
	var buf bytes.Buffer
	writeDrawio(&buf, drawioFixture())
	if err := xml.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	page := raw.Diagrams[4]
	labelOf := map[string]string{}
	for _, c := range page.Cells {
		labelOf[c.ID] = c.Value
	}
	found := false
	for _, c := range page.Cells {
		if c.Edge != "1" || c.Value != "Before" {
			continue
		}
		found = true
		if !strings.HasPrefix(labelOf[c.Source], "app.network") {
			t.Errorf("Before edge source = %q, want the directive's target app.network (it is the unit that waits)", labelOf[c.Source])
		}
		if !strings.HasPrefix(labelOf[c.Target], "app.container") {
			t.Errorf("Before edge target = %q, want the declaring unit app.container", labelOf[c.Target])
		}
	}
	if !found {
		t.Fatal("expected a Before edge on the fixture")
	}
}

// TestDrawioLegend: every page carries a legend covering exactly what that
// page draws — the kinds and edge styles on it, nothing document-global.
func TestDrawioLegend(t *testing.T) {
	var raw struct {
		Diagrams []struct {
			Name  string `xml:"name,attr"`
			Cells []struct {
				ID     string `xml:"id,attr"`
				Value  string `xml:"value,attr"`
				Parent string `xml:"parent,attr"`
				Vertex string `xml:"vertex,attr"`
				Edge   string `xml:"edge,attr"`
			} `xml:"mxGraphModel>root>mxCell"`
		} `xml:"diagram"`
	}
	var buf bytes.Buffer
	writeDrawio(&buf, drawioFixture())
	if err := xml.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	// Per page: kind swatches (unlabeled vertices) and edge samples. The
	// fixture draws: overview = hub boxes + one cross edge; networks =
	// containers/networks with an internal and a cross edge; storage = one
	// volume edge; detail = all five kinds with network/volume/image/cross;
	// deps = containers, a network, an external, with cross and deps edges.
	want := map[string][2]int{
		"1. Overview":     {1, 1},
		"2. Networks":     {2, 2},
		"3. Storage":      {2, 1},
		"4. Full detail":  {5, 4},
		"5. Dependencies": {3, 2},
	}
	for _, d := range raw.Diagrams {
		legendID := ""
		for _, c := range d.Cells {
			if c.Value == "Legend" {
				legendID = c.ID
			}
		}
		if legendID == "" {
			t.Errorf("%s has no Legend box", d.Name)
			continue
		}
		swatches, samples := 0, 0
		for _, c := range d.Cells {
			if c.Parent != legendID {
				continue
			}
			if c.Edge == "1" {
				samples++
			} else if c.Vertex == "1" && c.Value == "" {
				swatches++
			}
		}
		w := want[d.Name]
		if swatches != w[0] || samples != w[1] {
			t.Errorf("%s legend: %d swatches / %d samples, want %d / %d",
				d.Name, swatches, samples, w[0], w[1])
		}
	}
}

// TestDrawioOverviewLayeredFlow: the overview lays stacks out left-to-right
// by dependency depth — a pure producer sits in an earlier column than every
// stack it points at, and a shared network everyone attaches lands rightmost.
func TestDrawioOverviewLayeredFlow(t *testing.T) {
	c := func(stem, img string, nets ...any) eval.UnitRecord {
		return eval.UnitRecord{Kind: "container", Stem: stem, Filename: stem + ".container",
			Service: stem + ".service", Data: map[string]any{
				"imageString": img, "networkStrings": nets, "Container": map[string]any{},
			}}
	}
	nw := func(stem string) eval.UnitRecord {
		return eval.UnitRecord{Kind: "network", Stem: stem, Filename: stem + ".network",
			Service: stem + "-network.service", Data: map[string]any{}}
	}
	all := []eval.Quadlet{
		// proxy -> a and b; a -> infra; b -> infra: proxy leftmost, infra rightmost.
		{Name: "proxy", Units: []eval.UnitRecord{c("proxy", "docker.io/p:1", "a.network", "b.network")}},
		{Name: "a", Units: []eval.UnitRecord{c("a", "docker.io/a:1", "infra.network"), nw("a")}},
		{Name: "b", Units: []eval.UnitRecord{c("b", "docker.io/b:1", "infra.network"), nw("b")}},
		{Name: "infra", Units: []eval.UnitRecord{nw("infra"), c("infracd", "docker.io/i:1", "infra.network")}},
	}
	f := renderDrawio(t, buildGraph(all, all))
	overview := f.Diagrams[0]
	xOf := map[string]float64{}
	for _, cell := range overview.Model.Cells {
		if cell.Vertex == "1" && cell.Geo != nil && strings.HasPrefix(cell.Value, "<b>") {
			name := strings.TrimPrefix(cell.Value, "<b>")
			name = name[:strings.Index(name, "<")]
			xOf[name] = cell.Geo.X
		}
	}
	if !(xOf["proxy"] < xOf["a"] && xOf["proxy"] < xOf["b"]) {
		t.Errorf("proxy must sit left of the stacks it points at: %v", xOf)
	}
	if !(xOf["a"] < xOf["infra"] && xOf["b"] < xOf["infra"]) {
		t.Errorf("the shared network stack must sit rightmost: %v", xOf)
	}
}

// TestDrawioOverviewCycleStaysCompact: stacks joining each other's networks
// form a legal cycle; its members must share one column (SCC condensation)
// rather than inflating layer assignments to the iteration cap.
func TestDrawioOverviewCycleStaysCompact(t *testing.T) {
	c := func(stem, img string, nets ...any) eval.UnitRecord {
		return eval.UnitRecord{Kind: "container", Stem: stem, Filename: stem + ".container",
			Service: stem + ".service", Data: map[string]any{
				"imageString": img, "networkStrings": nets, "Container": map[string]any{},
			}}
	}
	nw := func(stem string) eval.UnitRecord {
		return eval.UnitRecord{Kind: "network", Stem: stem, Filename: stem + ".network",
			Service: stem + "-network.service", Data: map[string]any{}}
	}
	all := []eval.Quadlet{
		{Name: "a", Units: []eval.UnitRecord{c("a", "docker.io/a:1", "b.network"), nw("a")}},
		{Name: "b", Units: []eval.UnitRecord{c("b", "docker.io/b:1", "a.network"), nw("b")}},
		{Name: "feeder", Units: []eval.UnitRecord{c("feeder", "docker.io/f:1", "a.network")}},
	}
	f := renderDrawio(t, buildGraph(all, all))
	xOf := map[string]float64{}
	for _, cell := range f.Diagrams[0].Model.Cells {
		if cell.Vertex == "1" && cell.Geo != nil && strings.HasPrefix(cell.Value, "<b>") {
			name := strings.TrimPrefix(cell.Value, "<b>")
			name = name[:strings.Index(name, "<")]
			xOf[name] = cell.Geo.X
		}
	}
	if xOf["a"] != xOf["b"] {
		t.Errorf("cycle members must share a column: %v", xOf)
	}
	if !(xOf["feeder"] < xOf["a"]) {
		t.Errorf("the feeder must sit left of the cycle it points at: %v", xOf)
	}
}

// TestDrawioFlatRejected: stack boxes are the point of the drawio output.
func TestDrawioFlatRejected(t *testing.T) {
	dir := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {name: "app", units: #container: Container: {Image: "docker.io/img"}}
`)
	if out, err := runCmd(t, "--dir", dir, "graph", "--format", "drawio", "--flat"); err == nil {
		t.Fatalf("--flat with drawio must be rejected:\n%s", out)
	}
	out, err := runCmd(t, "--dir", dir, "graph", "--format", "drawio")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "<mxfile ") {
		t.Fatalf("expected mxfile output, got:\n%.120s", out)
	}
}
