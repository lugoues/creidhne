package cli

import (
	"io"
	"os"
	"testing"
)

// withStdin swaps os.Stdin for the duration of fn, feeding it the given input
// (empty string + closed pipe => immediate EOF).
func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	old := os.Stdin
	defer func() { os.Stdin = old }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	go func() {
		if input != "" {
			io.WriteString(w, input)
		}
		w.Close()
	}()
	fn()
}

func TestConfirmEOFIsError(t *testing.T) {
	withStdin(t, "", func() {
		if _, err := confirm("Apply?"); err == nil {
			t.Fatal("expected error when stdin yields no answer (EOF)")
		}
	})
}

func TestConfirmAnswers(t *testing.T) {
	cases := map[string]bool{"y\n": true, "yes\n": true, "n\n": false, "\n": false}
	for in, want := range cases {
		withStdin(t, in, func() {
			got, err := confirm("Apply?")
			if err != nil {
				t.Fatalf("%q: unexpected error %v", in, err)
			}
			if got != want {
				t.Errorf("%q: got %v want %v", in, got, want)
			}
		})
	}
}
