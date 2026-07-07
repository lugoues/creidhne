package eval_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestJSONLabelValidJSONRoundTrip proves #JSONLabel produces a Label= value that
// is (a) single-quoted so quadlet's word splitter keeps it one item, (b) free of
// any literal single quote that would terminate that quoting, and (c) valid JSON
// that decodes back to the original payload — even when a value contains ', <,
// >, & and ". This is the semantic guarantee the byte-equality golden fixture
// can't express.
func TestJSONLabelValidJSONRoundTrip(t *testing.T) {
	got := containerData(t, selfQuadlet(`{
		#container: Container: {Image: "img", Label: [
			q.#JSONLabel & {key: "app.spec", value: {note: "it's <b> & \"x\"", n: 3, ok: true}},
		]}
	}`), "labelStrings")
	if len(got) != 1 {
		t.Fatalf("want 1 label, got %d: %v", len(got), got)
	}
	s, ok := got[0].(string)
	if !ok {
		t.Fatalf("label is not a string: %T", got[0])
	}

	// (a) the whole key=value is wrapped in single quotes
	if !strings.HasPrefix(s, "'") || !strings.HasSuffix(s, "'") {
		t.Fatalf("label is not single-quoted: %q", s)
	}
	inner := s[1 : len(s)-1]

	// (b) no literal single quote survived inside (would break quadlet's quoting)
	if strings.Contains(inner, "'") {
		t.Fatalf("payload contains a literal single quote: %q", inner)
	}

	// (c) the value after key= is valid JSON that round-trips to the payload
	key, jsonPart, found := strings.Cut(inner, "=")
	if !found || key != "app.spec" {
		t.Fatalf("unexpected key= prefix: %q", inner)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &back); err != nil {
		t.Fatalf("label payload is not valid JSON: %v\n%s", err, jsonPart)
	}
	want := map[string]any{"note": `it's <b> & "x"`, "n": float64(3), "ok": true}
	if !reflect.DeepEqual(back, want) {
		t.Fatalf("round-trip mismatch:\n got  %#v\n want %#v", back, want)
	}
}
