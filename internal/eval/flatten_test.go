package eval_test

import (
	"reflect"
	"strings"
	"testing"
)

// TestNestedListsFlattenInDecode proves the Go half of list flattening: fields
// that templates range directly (no CUE-side xStrings comprehension) arrive
// flat in the decoded unit data. Environment is such a field; the schema
// admits one nesting level and the decode walk splices it.
func TestNestedListsFlattenInDecode(t *testing.T) {
	quads := loadSource(t, selfQuadlet(`{
		#container: {
			Container: {Image: "img", Environment: ["A=1", ["B=2", "C=3"], "D=4"]}
			Unit: After: ["a.service", ["b.service"]]
		}
	}`))
	data := quads[0].Units[0].Data
	env := data["Container"].(map[string]any)["Environment"]
	if want := []any{"A=1", "B=2", "C=3", "D=4"}; !reflect.DeepEqual(env, want) {
		t.Fatalf("Environment = %v, want %v", env, want)
	}
	after := data["Unit"].(map[string]any)["After"]
	if want := []any{"a.service", "b.service"}; !reflect.DeepEqual(after, want) {
		t.Fatalf("After = %v, want %v", after, want)
	}
}

// TestNestedListsFlattenInComprehensions proves the CUE half: xStrings
// comprehensions dispatch per element (scalar vs one-level list) because
// their consumers evaluate before Go decodes.
func TestNestedListsFlattenInComprehensions(t *testing.T) {
	got := containerData(t, selfQuadlet(`{
		#container: Container: {Image: "img", Label: ["a=b", ["c=d", "e=f"]]}
	}`), "labelStrings")
	want := []any{"a=b", "c=d", "e=f"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("labelStrings = %v, want %v", got, want)
	}
}

// TestRenderedSplicesInLabels proves the #Rendered contract is writable from
// a consumer package (this source is user CUE, not creidhne — a hidden field
// could never cross that boundary) and that #rendered accepts one label or a
// list, spliced flat wherever the helper sits, including in a nested list.
func TestRenderedSplicesInLabels(t *testing.T) {
	got := containerData(t, selfQuadlet(`{
		#container: Container: {
			Image: "img"
			Label: ["a=b", {#rendered: ["c=d", "e=f"]}, {#rendered: "g=h"}, [{#rendered: ["i=j"]}, "k=l"]]
		}
	}`), "labelStrings")
	want := []any{"a=b", "c=d", "e=f", "g=h", "i=j", "k=l"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("labelStrings = %v, want %v", got, want)
	}
}

// TestRenderedIncompleteFailsLoud: a struct in Label whose #rendered never
// becomes concrete must fail the render, not silently drop the label.
func TestRenderedIncompleteFailsLoud(t *testing.T) {
	err := loadSourceErr(t, selfQuadlet(`{
		#container: Container: {Image: "img", Label: [{key: "x"}]}
	}`))
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("want incomplete-value error, got: %v", err)
	}
}
