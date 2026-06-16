package eval

import (
	"strings"
	"testing"

	"cuelang.org/go/cue/cuecontext"
)

// cueError must list every underlying cue error with its position, not collapse
// to "<first> (and N more errors)" the way cue's default Error() does (which hid
// all but the first and made `crei validate` unhelpful on a cascade).
func TestCueErrorListsAllErrors(t *testing.T) {
	v := cuecontext.New().CompileString("a: 1 & 2\nb: 3 & 4\n")
	err := cueError("ctx", v.Validate())
	if err == nil {
		t.Fatal("expected a validation error")
	}
	msg := err.Error()
	if strings.Contains(msg, "more errors") {
		t.Errorf("error was truncated to the first; want every error:\n%s", msg)
	}
	for _, frag := range []string{"a:", "b:"} {
		if !strings.Contains(msg, frag) {
			t.Errorf("missing %q in:\n%s", frag, msg)
		}
	}

	if cueError("ctx", nil) != nil {
		t.Error("cueError(ctx, nil) should be nil")
	}
}
