package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func numberedLines(prefix string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s%d\n", prefix, i)
	}
	return b.String()
}

// TestDiffTruncatesLongInsert: a long added run keeps maxRun head/tail lines
// around a hidden-count marker; --verbose semantics (maxRun 0) shows all.
func TestDiffTruncatesLongInsert(t *testing.T) {
	old := "top\nbottom\n"
	new := "top\n" + numberedLines("line", 100) + "bottom\n"

	var buf bytes.Buffer
	renderInlineDiff(&buf, []byte(old), []byte(new), diffStyleHighlight, truncLimits{run: 10, threshold: 24})
	out := buf.String()
	for _, want := range []string{"+ line1", "+ line10", "+ line91", "+ line100", "(80 more lines; --verbose shows all)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "line50") {
		t.Fatalf("middle of the run should be hidden:\n%s", out)
	}

	buf.Reset()
	renderInlineDiff(&buf, []byte(old), []byte(new), diffStyleHighlight, truncLimits{})
	if !strings.Contains(buf.String(), "line50") || strings.Contains(buf.String(), "more lines") {
		t.Fatalf("maxRun 0 must show everything:\n%s", buf.String())
	}
}

// TestDiffShortRunNotTruncated: a run within the slack renders whole — hiding a
// couple of lines behind a marker is worse than showing them.
func TestDiffShortRunNotTruncated(t *testing.T) {
	old := "top\n"
	new := "top\n" + numberedLines("l", 22) // 22 <= 2*10+3
	var buf bytes.Buffer
	renderInlineDiff(&buf, []byte(old), []byte(new), diffStyleHighlight, truncLimits{run: 10, threshold: 24})
	if strings.Contains(buf.String(), "more lines") {
		t.Fatalf("run within slack must not truncate:\n%s", buf.String())
	}
}

// TestDiffTruncatesReplacePairs: an equal-length changed region truncates by
// pair, keeping each old/new line together, and the marker counts lines (2 per
// hidden pair).
func TestDiffTruncatesReplacePairs(t *testing.T) {
	old := numberedLines("v1-", 100)
	new := numberedLines("v2-", 100)
	var buf bytes.Buffer
	renderInlineDiff(&buf, []byte(old), []byte(new), diffStylePlain, truncLimits{run: 5, threshold: 14})
	out := buf.String()
	for _, want := range []string{"- v1-1", "+ v2-1", "- v1-100", "+ v2-100", "(180 more lines; --verbose shows all)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "v1-50") {
		t.Fatalf("hidden pairs should not render:\n%s", out)
	}
}

// TestDiffThresholdConfigurable: the engage point is its own knob — a run
// below the threshold renders whole even when far above 2*run, and truncation
// always hides at least one line (head/tail never overlap).
func TestDiffThresholdConfigurable(t *testing.T) {
	old := "top\n"
	new := "top\n" + numberedLines("l", 40)
	var buf bytes.Buffer
	renderInlineDiff(&buf, []byte(old), []byte(new), diffStyleHighlight, truncLimits{run: 5, threshold: 100})
	if strings.Contains(buf.String(), "more lines") {
		t.Fatalf("run below the threshold must not truncate:\n%s", buf.String())
	}
	buf.Reset()
	// Threshold lower than 2*run: the hidden>=1 guard still holds (21 lines,
	// run 10 -> 1 hidden).
	new = "top\n" + numberedLines("l", 21)
	renderInlineDiff(&buf, []byte(old), []byte(new), diffStyleHighlight, truncLimits{run: 10, threshold: 1})
	if !strings.Contains(buf.String(), "(1 more lines") {
		t.Fatalf("low threshold should engage with hidden>=1:\n%s", buf.String())
	}
}

// TestLimitsForScopesToImages: only images/ artifacts truncate; unit files get
// the zero limits (never truncated) no matter the config.
func TestLimitsForScopesToImages(t *testing.T) {
	cfg := config{ContextLines: 10, ContextThreshold: 24}
	if lim := limitsFor("images/app.context/dash.json", cfg); lim.run != 10 || lim.threshold != 24 {
		t.Fatalf("images artifact should carry the configured limits, got %+v", lim)
	}
	if lim := limitsFor("app.container", cfg); lim != (truncLimits{}) {
		t.Fatalf("unit files must never truncate, got %+v", lim)
	}
}
