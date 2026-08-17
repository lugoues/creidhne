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
	if len(f.Diagrams) != 4 {
		t.Fatalf("want 4 pages, got %d", len(f.Diagrams))
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

// TestDrawioRoundTrip: no relation is silently lost. Page 4 carries every
// kept resource edge, and its deps edges cover every non-resource pair that
// lacks a resource edge (merged per pair).
func TestDrawioRoundTrip(t *testing.T) {
	g := drawioFixture()
	r := reduceGraph(g)
	f := renderDrawio(t, g)
	detail := f.Diagrams[3]

	edges := 0
	for _, c := range detail.Model.Cells {
		if c.Edge == "1" {
			edges++
		}
	}
	wantResource := 0
	type pair struct{ from, to string }
	resourcePair := map[pair]bool{}
	depPairs := map[pair]bool{}
	for _, e := range r.keptEdges() {
		if relResource(e.Rel) {
			wantResource++
			resourcePair[pair{e.From, e.To}] = true
		}
	}
	// A deps pair renders unless every one of its directives is subsumed by a
	// resource edge on the same pair (only After/Requires/Wants are — a
	// Conflicts= or Before= toward a resource is real semantics).
	for _, e := range r.keptEdges() {
		if relResource(e.Rel) {
			continue
		}
		k := pair{e.From, e.To}
		if resourcePair[k] && subsumedByQuadlet[e.Rel] {
			continue
		}
		depPairs[k] = true
	}
	wantDeps := len(depPairs)
	if edges != wantResource+wantDeps {
		t.Fatalf("detail page has %d edges, want %d resource + %d merged deps", edges, wantResource, wantDeps)
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
