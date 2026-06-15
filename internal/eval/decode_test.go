package eval

import "testing"

// decodeJSONNumbers must reject manifest unit data that isn't a JSON object,
// rather than silently yielding a nil map (which would render an empty unit).
func TestDecodeJSONNumbersRejectsNonObject(t *testing.T) {
	for _, in := range []string{"null", "[1,2]", `"str"`, "5", "true"} {
		if _, err := decodeJSONNumbers([]byte(in)); err == nil {
			t.Errorf("decodeJSONNumbers(%q): want error, got nil", in)
		}
	}
}

// A valid object decodes, and integral numbers are coerced to int64 (so
// templates' {{ printf "%d" }} render integers, not %!d(float64=N)), recursing
// through nested structs and lists.
func TestDecodeJSONNumbersCoercesIntegers(t *testing.T) {
	m, err := decodeJSONNumbers([]byte(`{"n": 5, "nested": {"k": 7}, "list": [1, 2]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m["n"].(int64); !ok {
		t.Errorf("n: want int64, got %T", m["n"])
	}
	if _, ok := m["nested"].(map[string]any)["k"].(int64); !ok {
		t.Errorf("nested.k: want int64, got %T", m["nested"].(map[string]any)["k"])
	}
	if _, ok := m["list"].([]any)[0].(int64); !ok {
		t.Errorf("list[0]: want int64, got %T", m["list"].([]any)[0])
	}
}
