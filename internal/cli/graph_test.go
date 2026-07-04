package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/lugoues/creidhne/internal/eval"
)

const graphMain = `package config
import q "github.com/lugoues/creidhne@v0"
db: q.#Quadlet & {
	name: "db"
	units: #container: {Container: {Image: "docker.io/postgres:16", ContainerName: "db"}}
}
web: q.#Quadlet & {
	name: "web"
	units: {
		#build: {ContainerFile: "FROM alpine\n"}
		#container: {
			Container: {Image: units.#build.#self}
			Unit: After: [db.units.#container.#service]
		}
	}
}
`

func TestCmdGraphDot(t *testing.T) {
	dir := setupProject(t, graphMain)
	out, err := runCmd(t, "--dir", dir, "graph")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"digraph creidhne",
		`"web.container" -> "db.container" [label="After"`, // cross-quadlet ordering edge
		`"web.container" -> "web.build" [label="image"`,    // build-consume edge
		`"web.build" [shape=note]`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("dot output missing %q:\n%s", want, out)
		}
	}
}

func TestCmdGraphMermaid(t *testing.T) {
	dir := setupProject(t, graphMain)
	out, err := runCmd(t, "--dir", dir, "graph", "--format", "mermaid")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "graph LR") || !strings.Contains(out, "-.->|After|") || !strings.Contains(out, "-->|image|") {
		t.Fatalf("mermaid output unexpected:\n%s", out)
	}
}

func TestCmdGraphJSON(t *testing.T) {
	dir := setupProject(t, graphMain)
	out, err := runCmd(t, "--dir", dir, "graph", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Nodes []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"nodes"`
		Edges []struct {
			From, To, Rel string
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatalf("json did not parse: %v\n%s", err, out)
	}
	hasEdge := func(from, to, rel string) bool {
		for _, e := range g.Edges {
			if e.From == from && e.To == to && e.Rel == rel {
				return true
			}
		}
		return false
	}
	if !hasEdge("web.container", "db.container", "After") || !hasEdge("web.container", "web.build", "image") {
		t.Fatalf("json missing expected edges: %+v", g.Edges)
	}
}

func TestCmdGraphFilteredPullsInNeighbors(t *testing.T) {
	dir := setupProject(t, graphMain)
	// Focus on web: db.container is in another quadlet but is referenced, so it
	// must still appear (the neighborhood view) and stay grouped under its own
	// quadlet "db", not "web".
	out, err := runCmd(t, "--dir", dir, "graph", "web", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Nodes []struct{ ID, Quadlet string }
	}
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatalf("json did not parse: %v\n%s", err, out)
	}
	var found bool
	for _, n := range g.Nodes {
		if n.ID == "db.container" {
			found = true
			if n.Quadlet != "db" {
				t.Fatalf("pulled-in db.container should keep quadlet=db, got %q", n.Quadlet)
			}
		}
	}
	if !found {
		t.Fatalf("filtered graph should pull in the referenced db.container:\n%s", out)
	}
}

func TestCmdGraphGrouped(t *testing.T) {
	dir := setupProject(t, graphMain)

	dot, err := runCmd(t, "--dir", dir, "graph")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"subgraph cluster_", `label="db"`, `label="web"`} {
		if !strings.Contains(dot, want) {
			t.Fatalf("grouped dot missing %q:\n%s", want, dot)
		}
	}

	flat, err := runCmd(t, "--dir", dir, "graph", "--flat")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(flat, "subgraph") || strings.Contains(flat, "cluster_") {
		t.Fatalf("--flat should not cluster:\n%s", flat)
	}

	mer, err := runCmd(t, "--dir", dir, "graph", "--format", "mermaid")
	if err != nil {
		t.Fatal(err)
	}
	// Quadlets sort db before web.
	if !strings.Contains(mer, `subgraph sg0["db"]`) || !strings.Contains(mer, "end") {
		t.Fatalf("grouped mermaid missing subgraph blocks:\n%s", mer)
	}
}

// TestCmdGraphExternalGroup: external systemd targets are collected into one
// "external" cluster when grouped, and left loose under --flat.
func TestCmdGraphExternalGroup(t *testing.T) {
	dir := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: {Container: {Image: "docker.io/app"}, Unit: After: ["network-online.target"]}
}
`)
	dot, err := runCmd(t, "--dir", dir, "graph")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"subgraph cluster_external", `label="external"`, `"network-online.target"`} {
		if !strings.Contains(dot, want) {
			t.Fatalf("dot missing external group %q:\n%s", want, dot)
		}
	}

	mer, err := runCmd(t, "--dir", dir, "graph", "--format", "mermaid")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mer, `subgraph sgExt["external"]`) {
		t.Fatalf("mermaid missing external subgraph:\n%s", mer)
	}

	flat, err := runCmd(t, "--dir", dir, "graph", "--flat")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(flat, "cluster_external") {
		t.Fatalf("--flat should not emit an external cluster:\n%s", flat)
	}
}

func TestQuadletGroupsPartition(t *testing.T) {
	g := depGraph{nodes: map[string]graphNode{
		"web.container":         {ID: "web.container", Kind: "container", Quadlet: "web"},
		"web.build":             {ID: "web.build", Kind: "build", Quadlet: "web"},
		"db.container":          {ID: "db.container", Kind: "container", Quadlet: "db"},
		"network-online.target": {ID: "network-online.target", Kind: "external", External: true},
	}}
	order, byQ, ungrouped := g.quadletGroups()
	if !reflect.DeepEqual(order, []string{"db", "web"}) {
		t.Fatalf("order = %v, want [db web]", order)
	}
	if !reflect.DeepEqual(byQ["web"], []string{"web.build", "web.container"}) {
		t.Fatalf("web group = %v, want [web.build web.container]", byQ["web"])
	}
	if !reflect.DeepEqual(byQ["db"], []string{"db.container"}) {
		t.Fatalf("db group = %v", byQ["db"])
	}
	if !reflect.DeepEqual(ungrouped, []string{"network-online.target"}) {
		t.Fatalf("ungrouped = %v, want [network-online.target]", ungrouped)
	}
}

func TestCmdGraphInvalidFormat(t *testing.T) {
	dir := setupProject(t, graphMain)
	if _, err := runCmd(t, "--dir", dir, "graph", "--format", "svg"); err == nil {
		t.Fatal("expected an error for an unknown format")
	}
}

// buildResourceMain exercises a .build unit that references a managed network
// and volume via #self -- those flatten under the nested Build section, not the
// top level.
const buildResourceMain = `package config
import "github.com/lugoues/creidhne@v0"
infra: creidhne.#Quadlet & {name: "infra", units: {#network: {}, #volume: {}}}
img: creidhne.#Quadlet & {
	name: "img"
	units: #build: {
		ContainerFile: "FROM alpine\n"
		Build: {
			Network: [infra.units.#network.#self]
			Volume: [infra.units.#volume.#self & {target: "/data"}]
		}
	}
}
`

func graphEdges(t *testing.T, out string) []struct{ From, To, Rel string } {
	t.Helper()
	var g struct {
		Edges []struct{ From, To, Rel string }
	}
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatalf("json did not parse: %v\n%s", err, out)
	}
	return g.Edges
}

// TestCmdGraphBuildResourceEdges: a build unit's networks/volumes are nested
// under Build in the eval data; the graph must still emit those edges.
func TestCmdGraphBuildResourceEdges(t *testing.T) {
	dir := setupProject(t, buildResourceMain)
	out, err := runCmd(t, "--dir", dir, "graph", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var net, vol bool
	for _, e := range graphEdges(t, out) {
		if e.From == "img.build" && e.To == "infra.network" && e.Rel == "network" {
			net = true
		}
		if e.From == "img.build" && e.To == "infra.volume" && e.Rel == "volume" {
			vol = true
		}
	}
	if !net || !vol {
		t.Fatalf("build unit resource edges missing (network=%v volume=%v):\n%s", net, vol, out)
	}
}

// TestCmdGraphDedupEdges: a directive naming the same target twice must yield a
// single edge, not a duplicate.
func TestCmdGraphDedupEdges(t *testing.T) {
	dir := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
web: creidhne.#Quadlet & {
	name: "web"
	units: #container: {Container: {Image: "docker.io/nginx"}, Unit: After: ["ext.service", "ext.service"]}
}
`)
	out, err := runCmd(t, "--dir", dir, "graph", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range graphEdges(t, out) {
		if e.From == "web.container" && e.To == "ext.service" && e.Rel == "After" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly 1 After edge after dedup, got %d:\n%s", n, out)
	}
}

// TestCmdGraphMergesPairedEdges: Requires+After (and the like) between the same
// pair collapse to a single labeled edge in dot/mermaid, while JSON stays
// granular (one object per relationship).
func TestCmdGraphMergesPairedEdges(t *testing.T) {
	src := `package config
import "github.com/lugoues/creidhne@v0"
db: creidhne.#Quadlet & {name: "db", units: #container: Container: {Image: "docker.io/pg", ContainerName: "db"}}
web: creidhne.#Quadlet & {
	name: "web"
	units: #container: {
		Container: {Image: "docker.io/nginx"}
		Unit: {After: ["db.service"], Requires: ["db.service"]}
	}
}
`
	dir := setupProject(t, src)

	dot, err := runCmd(t, "--dir", dir, "graph", "--flat")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dot, `"web.container" -> "db.container" [label="After+Requires"`) {
		t.Fatalf("dot did not merge the paired edge:\n%s", dot)
	}
	// Merged edge with a requirement rel must render solid, not dashed.
	if strings.Contains(dot, `[label="After+Requires" style=dashed`) {
		t.Fatalf("merged requirement edge should be solid, not dashed:\n%s", dot)
	}

	mer, err := runCmd(t, "--dir", dir, "graph", "--format", "mermaid", "--flat")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mer, `-->|After+Requires|`) {
		t.Fatalf("mermaid did not merge the paired edge:\n%s", mer)
	}

	js, err := runCmd(t, "--dir", dir, "graph", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var after, requires bool
	for _, e := range graphEdges(t, js) {
		if e.From == "web.container" && e.To == "db.container" {
			after = after || e.Rel == "After"
			requires = requires || e.Rel == "Requires"
		}
	}
	if !after || !requires {
		t.Fatalf("json should keep After and Requires as separate edges (after=%v requires=%v):\n%s", after, requires, js)
	}
}

// TestCmdGraphMergedLabelDropsResourceToken: when a resource edge merges with
// hand-written directives, the resource kind is dropped from the label (the
// node shape conveys it), but a bare resource edge keeps its kind label.
func TestCmdGraphMergedLabelDropsResourceToken(t *testing.T) {
	dir := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
infra: creidhne.#Quadlet & {name: "infra", units: {#volume: {}, #network: {}}}
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: {
		Container: {
			Image: "docker.io/app"
			Volume: [infra.units.#volume.#self & {target: "/data"}]
			Network: [infra.units.#network.#self]
		}
		Unit: {After: [infra.units.#volume.#service], Requires: [infra.units.#volume.#service]}
	}
}
`)
	dot, err := runCmd(t, "--dir", dir, "graph", "--flat")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dot, `"app.container" -> "infra.volume" [label="After+Requires"]`) {
		t.Fatalf("mixed edge should drop the resource token from the label:\n%s", dot)
	}
	if strings.Contains(dot, "After+Requires+volume") {
		t.Fatalf("resource token was not dropped:\n%s", dot)
	}
	// The bare Network= edge (no hand-written directive) keeps its kind label.
	if !strings.Contains(dot, `"app.container" -> "infra.network" [label="network"`) {
		t.Fatalf("bare resource edge should keep its kind label:\n%s", dot)
	}
}

// TestCmdGraphDuplicateFilename: two units colliding on one filename must error
// (matching render), not silently misrender.
func TestCmdGraphDuplicateFilename(t *testing.T) {
	dir := setupProject(t, `package config
import "github.com/lugoues/creidhne@v0"
a: creidhne.#Quadlet & {name: "dup", units: #container: Container: Image: "docker.io/x"}
b: creidhne.#Quadlet & {name: "dup", units: #container: Container: Image: "docker.io/y"}
`)
	_, err := runCmd(t, "--dir", dir, "graph")
	if err == nil || !strings.Contains(err.Error(), "duplicate output file") {
		t.Fatalf("expected a duplicate-filename error, got: %v", err)
	}
}

// TestMermaidLabelEscapesQuote: a stray double quote (reachable only via a
// malformed external ref) must not break Mermaid's ["..."] quoting.
func TestMermaidLabelEscapesQuote(t *testing.T) {
	if got := mermaidLabel(`we"b.target`); strings.Contains(got, `"`) {
		t.Fatalf("mermaidLabel left a raw quote: %q", got)
	}
	g := depGraph{nodes: map[string]graphNode{
		`we"b.target`: {ID: `we"b.target`, Kind: "external", External: true},
	}}
	var buf strings.Builder
	writeMermaid(&buf, g, true)
	if strings.Contains(buf.String(), `\"`) {
		t.Fatalf("mermaid output has an unescaped/backslash quote:\n%s", buf.String())
	}
}

// TestBuildGraphExternalTarget checks that an unmanaged [Unit] target becomes a
// distinct external node rather than being dropped.
func TestBuildGraphExternalTarget(t *testing.T) {
	u := eval.UnitRecord{
		Kind: "container", Stem: "app", Filename: "app.container", Service: "app.service",
		Data: map[string]any{"Unit": map[string]any{"After": []any{"network-online.target"}}},
	}
	g := buildGraph([]eval.Quadlet{{Name: "app", Units: []eval.UnitRecord{u}}}, []eval.Quadlet{{Name: "app", Units: []eval.UnitRecord{u}}})
	n, ok := g.nodes["network-online.target"]
	if !ok || !n.External {
		t.Fatalf("expected an external node for network-online.target, got %+v (ok=%v)", n, ok)
	}
	if len(g.edges) != 1 || g.edges[0].Rel != "After" || g.edges[0].To != "network-online.target" {
		t.Fatalf("unexpected edges: %+v", g.edges)
	}
}
