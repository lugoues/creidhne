package cli

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/pmezard/go-difflib/difflib"
)

var (
	// File header: bold, neutral color, so it stands apart from the green/red
	// diff body (Terraform's "# name" style).
	diffHeaderStyle = lipgloss.NewStyle().Bold(true)
	// Collapsed-context marker: gray and light.
	diffHiddenStyle = lipgloss.NewStyle().Faint(true).Italic(true)
	// Unchanged context lines: gray so changes stand out (configurable).
	diffContextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorContext))
	// Unchanged text within a modified row -- its own knob; defaults to the
	// normal text style (terminal default), distinct from the context lines above.
	inlineContextStyle = lipgloss.NewStyle()
	// On a modified line the unchanged text uses inlineContextStyle; only the
	// changed run is colored (bold red removed / bold green added) so it pops.
	delSpanStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorRemove)).Bold(true)
	addSpanStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAdd)).Bold(true)
)

// diffIndent tabs the diff body in under the "# name" header so the gutter signs
// don't align with the hash.
const diffIndent = "  "

// bodyln writes one indented diff body line (the header is not indented).
func bodyln(w io.Writer, s string) { fmt.Fprintln(w, diffIndent+s) }

// renderInlineDiff writes a unified-style line diff of old vs new: the +/- sign
// sits in its own gutter column, changed lines are highlighted inline (the
// differing span between the common prefix and suffix is emphasized), and runs
// of unchanged lines outside the context window collapse to a gray
// "# (N unmodified lines hidden)" marker.
func renderInlineDiff(w io.Writer, old, new []byte, style string) {
	a, b := splitLines(string(old)), splitLines(string(new))
	prevEnd := 0
	emitHidden := func(n int) {
		if n > 0 {
			bodyln(w, diffHiddenStyle.Render(fmt.Sprintf("# (%d unmodified line%s hidden)", n, plural(n))))
		}
	}
	for _, group := range difflib.NewMatcher(a, b).GetGroupedOpCodes(3) {
		emitHidden(group[0].I1 - prevEnd)
		for _, op := range group {
			switch op.Tag {
			case 'e': // equal: dimmed context, blank gutter
				for _, l := range a[op.I1:op.I2] {
					bodyln(w, diffContextStyle.Render("  "+l))
				}
			case 'd': // delete
				for _, l := range a[op.I1:op.I2] {
					bodyln(w, red("- "+l))
				}
			case 'i': // insert
				for _, l := range b[op.J1:op.J2] {
					bodyln(w, green("+ "+l))
				}
			case 'r': // replace
				renderReplace(w, a[op.I1:op.I2], b[op.J1:op.J2], style)
			}
		}
		prevEnd = group[len(group)-1].I2
	}
	emitHidden(len(a) - prevEnd)
}

// renderReplace shows a changed region. When the same number of lines changed,
// each old/new pair is rendered per the configured style; otherwise it's a block
// rewrite with no 1:1 pairing, always shown as plain removed-then-added lines.
func renderReplace(w io.Writer, oldLines, newLines []string, style string) {
	if len(oldLines) != len(newLines) {
		for _, l := range oldLines {
			bodyln(w, red("- "+l))
		}
		for _, l := range newLines {
			bodyln(w, green("+ "+l))
		}
		return
	}
	for i := range oldLines {
		switch style {
		case diffStyleInline:
			bodyln(w, inlineSingle(oldLines[i], newLines[i]))
		case diffStylePlain:
			bodyln(w, red("- "+oldLines[i]))
			bodyln(w, green("+ "+newLines[i]))
		default: // diffStyleHighlight
			del, ins := inlineHighlight(oldLines[i], newLines[i])
			bodyln(w, del)
			bodyln(w, ins)
		}
	}
}

// diffSpans splits a changed line pair into the shared prefix, the differing
// middle on each side, and the shared suffix. Rune-aligned, so multibyte text is
// never split mid-character.
func diffSpans(oldLine, newLine string) (pre, oldMid, newMid, suf string) {
	o, n := []rune(oldLine), []rune(newLine)
	p := 0
	for p < len(o) && p < len(n) && o[p] == n[p] {
		p++
	}
	s := 0
	for s < len(o)-p && s < len(n)-p && o[len(o)-1-s] == n[len(n)-1-s] {
		s++
	}
	return string(o[:p]), string(o[p : len(o)-s]), string(n[p : len(n)-s]), string(o[len(o)-s:])
}

// inlineHighlight renders a changed line pair ("- old" / "+ new"): the gutter
// sign is colored, the changed run is colored (bold), and the unchanged text
// uses the context style, so only the change draws the eye.
func inlineHighlight(oldLine, newLine string) (del, ins string) {
	pre, oldMid, newMid, suf := diffSpans(oldLine, newLine)
	preS, sufS := emph(inlineContextStyle, pre), emph(inlineContextStyle, suf)
	del = red("- ") + preS + emph(delSpanStyle, oldMid) + sufS
	ins = green("+ ") + preS + emph(addSpanStyle, newMid) + sufS
	return del, ins
}

// inlineSingle renders a changed line as one "~" line, word-diff style: the
// unchanged text uses the context style, then at the point of change the removed
// run is struck through in the remove color followed by the added run in the add
// color. So both sides show on one line (e.g. "...sock:r̶o̶w" for ro->rw,
// "...ALL L̶L̶L̶" for ALLLLL->ALL, "...Pr s oxy" for Proxy->Prsoxy).
func inlineSingle(oldLine, newLine string) string {
	pre, oldMid, newMid, suf := diffSpans(oldLine, newLine)
	return yellow("~ ") + emph(inlineContextStyle, pre) +
		emph(delSpanStyle.Strikethrough(true), oldMid) +
		emph(addSpanStyle, newMid) + emph(inlineContextStyle, suf)
}

// emph styles s, returning "" for an empty span so no stray escape is emitted.
func emph(style lipgloss.Style, s string) string {
	if s == "" {
		return ""
	}
	return style.Render(s)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
