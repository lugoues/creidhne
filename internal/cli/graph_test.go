package cli

import (
	"encoding/json"
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
	// must still appear (the neighborhood view).
	out, err := runCmd(t, "--dir", dir, "graph", "web", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "db.container") {
		t.Fatalf("filtered graph should pull in the referenced db.container:\n%s", out)
	}
}

func TestCmdGraphInvalidFormat(t *testing.T) {
	dir := setupProject(t, graphMain)
	if _, err := runCmd(t, "--dir", dir, "graph", "--format", "svg"); err == nil {
		t.Fatal("expected an error for an unknown format")
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
