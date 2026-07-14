// Error translation: collapse cue's disjunction noise, humanize contract
// paths, name the schema's constraints, and suggest fixes for typos. See
// docs/design/error-translation.md. Matchers are conservative: anything
// unrecognized falls through to the raw output, which is always appended.
package eval

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"

	"github.com/lugoues/creidhne"
)

// finding is one translated error.
type finding struct {
	loc string   // humanized location
	msg string   // actionable one-liner
	pos []string // filtered source positions, user files first
}

// DiagnosticError separates translated findings from the raw cue detail so
// the CLI can style them apart (findings loud, detail dim). Error() joins
// them for any non-styling consumer.
type DiagnosticError struct {
	Findings string
	Detail   string
}

func (e *DiagnosticError) Error() string {
	if e.Detail == "" {
		return e.Findings
	}
	return e.Findings + "\n\n" + e.Detail
}

// diagError renders translated findings; the raw detail is carried only
// when translation was lossy (arms dropped, messages rewritten, positions
// withheld). A faithful pass-through stands alone, so a plain error is
// never printed twice. dir relativizes positions.
func diagError(root cue.Value, dir, context string, err error) error {
	if err == nil {
		return nil
	}
	findings, lossy := diagnose(root, dir, err)
	if len(findings) == 0 {
		return cueError(context, err)
	}
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "%s: %s\n", f.loc, f.msg)
		for _, p := range f.pos {
			fmt.Fprintf(&b, "    %s\n", p)
		}
	}
	de := &DiagnosticError{Findings: strings.TrimRight(b.String(), "\n")}
	if lossy {
		de.Detail = cueError(context, err).Error()
	}
	return de
}

// diagnose groups the flattened errors by path, collapses disjunction arms
// to the most informative survivor, and translates what it recognizes.
// lossy reports whether any information was dropped or rewritten.
func diagnose(root cue.Value, dir string, err error) ([]finding, bool) {
	groups := map[string][]errors.Error{}
	var order []string
	for _, e := range errors.Errors(err) {
		key := strings.Join(e.Path(), ".")
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], e)
	}

	lossy := false
	// A group that only announces "N errors in empty disjunction" is
	// bookkeeping for deeper groups; drop it when any other group exists.
	informative := map[string]errors.Error{}
	for key, es := range groups {
		if len(es) > 1 {
			lossy = true // sibling arms dropped
		}
		if best := bestArm(es); best != nil {
			informative[key] = best
		}
	}
	// A failing list element is one mistake, but every disjunction arm
	// probes a different field of the value, each rejection with its own
	// deeper path. Collapse everything under a disjunction element into the
	// element's own finding.
	collapsed := map[string]bool{}
	for key := range groups {
		anchor := anchorOf(key)
		if anchor == "" || anchor == key {
			continue
		}
		if aes, ok := groups[anchor]; !ok || len(aes) < 2 {
			continue // anchor is not a disjunction element
		}
		if _, live := informative[key]; live {
			delete(informative, key)
			collapsed[anchor] = true
			lossy = true
		}
	}
	// Parent headers whose children carry the real error.
	for key := range informative {
		for other := range informative {
			if other != key && strings.HasPrefix(other, key+".") && isHeader(informative[key]) {
				delete(informative, key)
				lossy = true
				break
			}
		}
	}

	var out []finding
	for _, key := range order {
		e, ok := informative[key]
		if !ok || isHeader(e) && len(informative) > 1 {
			continue
		}
		// Comprehension-internal errors (the xStrings dispatch) carry
		// numeric-only or empty paths. When the error still touches the
		// user's files, locate it by its positions: the dispatch guard line
		// in the embedded schema names the source field (Container.Volume),
		// and the user position pins the entry. Without user positions it is
		// pure dispatch noise and stays in the raw detail.
		if !hasNamedSegment(e.Path()) {
			lossy = true
			if f, ok := locateByPosition(dir, e); ok {
				out = append(out, f)
			}
			continue
		}
		f, rewrote := translate(root, dir, e)
		if collapsed[key] {
			f.msg = "matches no accepted form here: " + f.msg
			if structInRefSlot(strings.Split(key, "."), msgOf(e)) {
				f.msg += ` (a struct here is built from the unit's ".#self" handle)`
			}
			rewrote = true
		}
		lossy = lossy || rewrote
		out = append(out, f)
	}
	return out, lossy
}

// refSlots are the unit-section list fields whose entries reference other
// units; a struct rejected there is a unit (or a bare decoration) missing
// its ".#self" base. The element value itself is unreachable through the
// conflict, so the hint keys on the slot and the printed value's shape.
var refSlots = map[string]bool{"Network": true, "Volume": true, "Mount": true, "Secret": true, "Tmpfs": true}

func structInRefSlot(segs []string, msg string) bool {
	if len(segs) < 2 {
		return false
	}
	return refSlots[segs[len(segs)-2]] && strings.Contains(msg, "conflicting values {")
}

// anchorOf is the path up to and including its first list index: the
// element a disjunction is testing. Empty when the path has no index.
func anchorOf(key string) string {
	segs := strings.Split(key, ".")
	for i, s := range segs {
		if _, err := strconv.Atoi(s); err == nil {
			return strings.Join(segs[:i+1], ".")
		}
	}
	return ""
}


// hasNamedSegment reports whether a path contains at least one identifier
// segment (not just list indexes).
func hasNamedSegment(p []string) bool {
	for _, s := range p {
		for _, r := range s {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
				return true
			}
		}
	}
	return false
}

func msgOf(e errors.Error) string {
	format, args := e.Msg()
	return fmt.Sprintf(format, args...)
}

func isHeader(e errors.Error) bool {
	return strings.Contains(msgOf(e), "errors in empty disjunction")
}

// bestArm picks the most informative error of a same-path group: constraint
// violations beat closedness beats other conflicts beats incompleteness;
// the guaranteed-noise nested-list arm ("mismatched types X and list") and
// disjunction headers lose to anything.
func bestArm(es []errors.Error) errors.Error {
	rank := func(e errors.Error) int {
		m := msgOf(e)
		switch {
		case strings.Contains(m, "errors in empty disjunction"):
			return 0
		case strings.Contains(m, "mismatched types") && strings.Contains(m, "and list"):
			return 1
		case strings.Contains(m, "incomplete value"):
			return 2
		case strings.Contains(m, "mismatched types"):
			return 3
		case strings.Contains(m, "field not allowed"):
			return 5
		case strings.Contains(m, "out of bound") || strings.Contains(m, "invalid value"):
			return 6
		default:
			return 4
		}
	}
	var best errors.Error
	bestRank := -1
	for _, e := range es {
		if r := rank(e); r > bestRank {
			best, bestRank = e, r
		}
	}
	return best
}

// xStrings maps the schema's rendering comprehensions back to the source
// field the user actually wrote (two-edit rule with the *.cue comprehensions).
var xStrings = map[string]string{
	"labelStrings":   "Label",
	"volumeStrings":  "Volume",
	"networkStrings": "Network",
	"tmpfsStrings":   "Tmpfs",
	"mountStrings":   "Mount",
	"secretStrings":  "Secret",
	"ulimitStrings":  "Ulimit",
	"userNSString":   "UserNS",
	"podString":      "Pod",
	"imageString":    "Image",
}

// translate builds the finding for one error: humanized location, named
// constraint or suggestion, filtered positions. rewrote reports whether the
// finding no longer carries the full raw message and positions verbatim.
func translate(root cue.Value, dir string, e errors.Error) (finding, bool) {
	p := e.Path()
	raw := msgOf(e)
	msg := conciseMsg(raw)
	pos, dropped := filterPositions(dir, e)
	f := finding{loc: humanizePath(root, p), msg: msg, pos: pos}
	rewrote := msg != raw || dropped

	if name, hint, ok := constraintFor(msg); ok {
		switch {
		case strings.Contains(msg, "incomplete value"):
			f.msg = fmt.Sprintf("never resolved to %s (%s); an unset helper or required field upstream", hint, name)
		default:
			if v, ok := quotedValue(msg); ok {
				f.msg = fmt.Sprintf("%s is not %s (%s)", v, hint, name)
			} else {
				f.msg = fmt.Sprintf("not %s (%s)", hint, name)
			}
		}
		return f, true
	}
	if strings.Contains(msg, "field not allowed") && len(p) > 0 {
		if sugg := nearestField(root, p); sugg != "" {
			f.msg = fmt.Sprintf("field not allowed; did you mean %q?", sugg)
		}
		return f, true
	}
	return f, rewrote
}

// constraintFor names the schema's load-bearing constraints when their
// literal appears in the message.
func constraintFor(msg string) (name, hint string, ok bool) {
	for _, c := range []struct{ needle, name, hint string }{
		{`=~"^[^=]+=.*$"`, "#KeyValue", `a "key=value" pair`},
		{`(service|socket|target|timer|path|mount|automount|device|swap|slice|scope)$`, "#ServiceName", "a systemd unit name (*.service, *.target, ...)"},
		{`(/(tcp|udp|sctp))?$`, "#PortMapping", "a port mapping ([ip:]host[:container][/proto])"},
		{`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`, "#UnitName", "a safe unit name (letters, digits, _ . -)"},
		{`=(-1|[0-9]+)(:(-1|[0-9]+))?$`, "#Ulimit", `"name=soft[:hard]" or "host"`},
	} {
		if strings.Contains(msg, c.needle) {
			return c.name, c.hint, true
		}
	}
	return "", "", false
}

// bottomRe matches a bottom embedded in a printed value:
// _|_(1.1: invalid interpolation: required field missing: port).
var bottomRe = regexp.MustCompile(`_\|_\(([^()]*(?:\([^()]*\)[^()]*)*)\)`)

// numPathPrefixRe strips the useless numeric path off an embedded reason.
var numPathPrefixRe = regexp.MustCompile(`^[\d.]+: `)

// conciseMsg reduces a raw message to its actionable core: reasons embedded
// as bottoms inside printed values win (they carry the real cause), then
// struct dumps are trimmed.
func conciseMsg(m string) string {
	var reasons []string
	seen := map[string]bool{}
	for _, g := range bottomRe.FindAllStringSubmatch(m, -1) {
		r := numPathPrefixRe.ReplaceAllString(strings.TrimSpace(g[1]), "")
		if r != "" && !seen[r] {
			seen[r] = true
			reasons = append(reasons, r)
		}
	}
	if len(reasons) > 0 {
		return strings.Join(reasons, "; ")
	}
	return trimMsg(m)
}

// trimMsg cuts struct dumps out of long messages (cue prints the whole
// resolved struct for unsatisfied disjunctions); short messages keep their
// literals (a user's small struct is the evidence, not noise). A cut that
// would leave a contentless stub falls back to plain truncation.
func trimMsg(m string) string {
	if len(m) <= 200 {
		return m
	}
	if i := strings.IndexByte(m, '{'); i >= 40 {
		return strings.TrimSpace(m[:i]) + "…"
	}
	return m[:200] + "…"
}

// quotedValue extracts the offending literal from `invalid value "X" (...)`
// or `conflicting values "X" and ...`.
func quotedValue(msg string) (string, bool) {
	i := strings.IndexByte(msg, '"')
	if i < 0 {
		return "", false
	}
	j := strings.IndexByte(msg[i+1:], '"')
	if j < 0 {
		return "", false
	}
	return msg[i : i+j+2], true
}

// humanizePath rewrites an error path into user coordinates: quadlet name,
// unit file, source field. Unknown shapes pass through joined.
func humanizePath(root cue.Value, p []string) string {
	if len(p) == 0 {
		return "(root)"
	}
	quad := strings.Trim(p[0], `"`)
	rest := p[1:]

	// manifest.N.data.<field...>: resolve the unit filename from the manifest.
	// root is the instance (path prefixed by the quadlet) or the quadlet
	// itself (tryQuadlet); try both scopes.
	if len(rest) >= 3 && rest[0] == "manifest" && rest[2] == "data" {
		if idx, err := strconv.Atoi(rest[1]); err == nil {
			file, _ := root.LookupPath(cue.MakePath(cue.Str(quad), cue.Str("manifest"), cue.Index(idx), cue.Str("filename"))).String()
			if file == "" {
				file, _ = root.LookupPath(cue.MakePath(cue.Str("manifest"), cue.Index(idx), cue.Str("filename"))).String()
			}
			if file != "" {
				return fmt.Sprintf("%s (%s): %s", quad, file, fieldPath(rest[3:]))
			}
		}
	}
	// units.<unit>.<field...>: derive the unit file from the segment.
	if len(rest) >= 2 && rest[0] == "units" {
		unit := rest[1]
		field := rest[2:]
		kindOf := func(seg string) string {
			seg = strings.TrimPrefix(seg, "#")
			return strings.TrimSuffix(seg, "s") // containers -> container
		}
		switch {
		case strings.HasPrefix(unit, "#"):
			return fmt.Sprintf("%s (%s.%s): %s", quad, quad, kindOf(unit), fieldPath(field))
		case len(field) >= 1:
			key := strings.Trim(field[0], `"`)
			return fmt.Sprintf("%s (%s-%s.%s): %s", quad, quad, key, kindOf(unit), fieldPath(field[1:]))
		}
	}
	return quad + ": " + fieldPath(rest)
}

// fieldPath renders trailing path segments, folding xStrings names back to
// their source field and indexes into brackets.
func fieldPath(segs []string) string {
	var b strings.Builder
	for _, s := range segs {
		if src, ok := xStrings[s]; ok {
			s = src + " (flattened)"
		}
		if n, err := strconv.Atoi(s); err == nil {
			fmt.Fprintf(&b, "[%d]", n)
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(strings.Trim(s, `"`))
	}
	if b.Len() == 0 {
		return "(value)"
	}
	return b.String()
}

// nearestField suggests the closest allowed field for a closedness error.
// A disjunction parent (e.g. Container's Image|Rootfs variants) contributes
// every arm's fields.
func nearestField(root cue.Value, p []string) string {
	bad := strings.Trim(p[len(p)-1], `"`)
	parent := lookupSegments(root, p[:len(p)-1])
	if !parent.Exists() {
		return ""
	}
	names := map[string]bool{}
	gatherFields(parent, 3, names)
	best, bestDist := "", 3 // suggest only close typos
	for name := range names {
		if name == bad {
			continue // the rejected field itself is in the failing struct
		}
		if d := editDistance(strings.ToLower(bad), strings.ToLower(name)); d < bestDist {
			best, bestDist = name, d
		}
	}
	return best
}

// gatherFields unions field names from a value and, through unification and
// disjunction nodes, its expression operands: a failing parent is often
// `userStruct & (variantA | variantB)`, where the allowed set lives in the
// variants.
func gatherFields(v cue.Value, depth int, out map[string]bool) {
	if !v.Exists() || depth == 0 {
		return
	}
	if it, err := v.Fields(cue.Optional(true)); err == nil {
		for it.Next() {
			out[it.Selector().Unquoted()] = true
		}
	}
	if op, args := v.Expr(); op == cue.AndOp || op == cue.OrOp {
		for _, a := range args {
			gatherFields(a, depth-1, out)
		}
	}
}

// lookupSegments builds a cue.Path from raw error-path segments (indexes,
// definitions, quoted keys) and resolves it, falling back one level at a
// time when a segment is not addressable.
func lookupSegments(root cue.Value, segs []string) cue.Value {
	sel := make([]cue.Selector, 0, len(segs))
	for _, s := range segs {
		if s == "" {
			continue
		}
		if n, err := strconv.Atoi(s); err == nil {
			sel = append(sel, cue.Index(n))
			continue
		}
		if strings.HasPrefix(s, "#") {
			sel = append(sel, cue.Def(s))
			continue
		}
		sel = append(sel, cue.Str(strings.Trim(s, `"`)))
	}
	return root.LookupPath(cue.MakePath(sel...))
}

func editDistance(a, b string) int {
	la, lb := len(a), len(b)
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}

const schemaPosMarker = "/cue.mod/usr/github.com/lugoues/creidhne/"

// schemaFieldRe matches a dispatch guard line ("if Container.Volume != _|_
// for e in Container.Volume {...") to name the field it renders.
var schemaFieldRe = regexp.MustCompile(`\b(Container|Pod|Network|Volume|Build|Kube|Image|Artifact)\.([A-Za-z]\w*)\b`)

// locateByPosition builds a finding for a pathless dispatch error from its
// positions: the source field comes from the dispatch guard line in the
// embedded schema, the location from the user's own position. Errors with
// no user position are dispatch probe noise and get no finding.
func locateByPosition(dir string, e errors.Error) (finding, bool) {
	var user []string
	field := ""
	seen := map[string]bool{}
	scan := func(pos token.Pos) {
		if !pos.IsValid() {
			return
		}
		s := pos.String()
		if seen[s] {
			return
		}
		seen[s] = true
		if i := strings.Index(s, schemaPosMarker); i >= 0 {
			if field == "" {
				field = fieldFromSchemaLine(s[i+len(schemaPosMarker):])
			}
			return
		}
		if rel := strings.TrimPrefix(s, dir+"/"); rel != s {
			s = rel
		}
		user = append(user, s)
	}
	scan(e.Position())
	for _, p := range e.InputPositions() {
		scan(p)
	}
	if len(user) == 0 {
		return finding{}, false
	}
	sort.Strings(user)
	loc := "a list entry"
	if field != "" {
		loc = field + " entry"
	}
	return finding{loc: loc, msg: conciseMsg(msgOf(e)), pos: user}, true
}

// fieldFromSchemaLine resolves "container.cue:248:4" against the embedded
// schema and extracts the section field the surrounding comprehension
// reads, scanning upward from the position to the comprehension's guard
// line (dispatch positions usually point at the inner branch lines).
func fieldFromSchemaLine(ref string) string {
	parts := strings.SplitN(ref, ":", 3)
	if len(parts) < 2 {
		return ""
	}
	line, err := strconv.Atoi(parts[1])
	if err != nil || line < 1 {
		return ""
	}
	raw, err := fs.ReadFile(creidhne.SchemaFS, "creidhne/"+parts[0])
	if err != nil {
		return ""
	}
	lines := strings.Split(string(raw), "\n")
	if line > len(lines) {
		return ""
	}
	for i := line - 1; i >= 0 && i >= line-9; i-- {
		if m := schemaFieldRe.FindStringSubmatch(lines[i]); m != nil {
			return m[1] + "." + m[2]
		}
	}
	return ""
}

// filterPositions keeps positions in the user's project (relativized) plus
// at most one schema position for the constraint definition. dropped reports
// whether any schema positions were withheld.
func filterPositions(dir string, e errors.Error) ([]string, bool) {
	seen := map[string]bool{}
	var user, schema []string
	add := func(pos token.Pos) {
		if !pos.IsValid() {
			return
		}
		s := pos.String()
		if seen[s] {
			return
		}
		seen[s] = true
		if strings.Contains(s, "/cue.mod/") {
			schema = append(schema, s)
			return
		}
		if rel := strings.TrimPrefix(s, dir+"/"); rel != s {
			s = rel
		}
		user = append(user, s)
	}
	add(e.Position())
	for _, p := range e.InputPositions() {
		add(p)
	}
	sort.Strings(user)
	if len(schema) > 0 {
		user = append(user, schema[0])
	}
	return user, len(schema) > 1
}
