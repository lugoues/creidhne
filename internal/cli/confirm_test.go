package cli

import (
	"io"
	"strings"
	"testing"
)

func TestConfirmEOFIsError(t *testing.T) {
	// Empty input => immediate EOF => error, so callers fail loudly rather than
	// treating a non-interactive run as a silent "no".
	if _, err := confirm(strings.NewReader(""), io.Discard, "Apply?"); err == nil {
		t.Fatal("expected error when stdin yields no answer (EOF)")
	}
}

func TestConfirmAnswers(t *testing.T) {
	cases := map[string]bool{"y\n": true, "yes\n": true, "n\n": false, "\n": false}
	for in, want := range cases {
		got, err := confirm(strings.NewReader(in), io.Discard, "Apply?")
		if err != nil {
			t.Fatalf("%q: unexpected error %v", in, err)
		}
		if got != want {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
}
