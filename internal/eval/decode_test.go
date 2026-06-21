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

// An integer too large for int64 must be rejected, not silently degraded to a
// float64 (which renders as %!d(float64=...) in a {{ printf "%d" }} field like
// UID/GID/HealthRetries). A generator must fail loudly, not emit a corrupt unit.
func TestDecodeJSONNumbersRejectsOverflowInteger(t *testing.T) {
	if m, err := decodeJSONNumbers([]byte(`{"UID": 99999999999999999999}`)); err == nil {
		t.Fatalf("want error for int64-overflow integer, got %#v (%T)", m["UID"], m["UID"])
	}
}

// The boundary value (max int64) must still be accepted as an int64.
func TestDecodeJSONNumbersAcceptsMaxInt64(t *testing.T) {
	m, err := decodeJSONNumbers([]byte(`{"n": 9223372036854775807}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := m["n"].(int64); !ok || got != 9223372036854775807 {
		t.Errorf("n: got %#v (%T), want int64 9223372036854775807", m["n"], m["n"])
	}
}
