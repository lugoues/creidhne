package cli

import (
	"strings"
	"testing"

	"github.com/lugoues/creidhne/internal/eval"
)

// pairNetwork builds a network unit carrying the pair marker label.
func pairNetwork(quadlet, stem string) eval.Quadlet {
	return eval.Quadlet{Name: quadlet, Units: []eval.UnitRecord{{
		Kind: "network", Stem: stem, Filename: stem + ".network",
		Data: map[string]any{"labelStrings": []any{"creidhne.pair=" + stem}},
	}}}
}

func attacherQuadlet(quadlet, stem string, networks ...any) eval.Quadlet {
	return eval.Quadlet{Name: quadlet, Units: []eval.UnitRecord{{
		Kind: "container", Stem: stem, Filename: stem + ".container",
		Data: map[string]any{"networkStrings": networks, "Container": map[string]any{}},
	}}}
}

// TestPairCardinalityDeduplicatesAttachers: one external unit listing the pair
// network twice is still one attacher — not a phantom cardinality breach.
func TestPairCardinalityDeduplicatesAttachers(t *testing.T) {
	quads := []eval.Quadlet{
		pairNetwork("app", "app-pair"),
		attacherQuadlet("app", "app-web", "app-pair.network"),
		attacherQuadlet("proxy", "traefik", "app-pair.network", "app-pair.network:alias=web"),
	}
	for _, f := range graphRuleFindings(quads) {
		if f.Rule == "graph/pair-cardinality" {
			t.Fatalf("duplicate listing by one unit must not breach cardinality: %+v", f)
		}
	}
}

// TestKubeUnitsAttachNetworks: a network consumed only through a kube unit's
// [Kube] Network= is attached, not orphaned.
func TestKubeUnitsAttachNetworks(t *testing.T) {
	quads := []eval.Quadlet{
		{Name: "infra", Units: []eval.UnitRecord{{
			Kind: "network", Stem: "infra", Filename: "infra.network",
			Data: map[string]any{},
		}}},
		{Name: "app", Units: []eval.UnitRecord{{
			Kind: "kube", Stem: "app", Filename: "app.kube",
			Data: map[string]any{"Kube": map[string]any{"Network": []any{"infra.network"}}},
		}}},
	}
	for _, f := range graphRuleFindings(quads) {
		if f.Rule == "graph/orphan-network" && strings.Contains(f.Unit, "infra.network") {
			t.Fatalf("kube-attached network misreported as orphan: %+v", f)
		}
	}
}
