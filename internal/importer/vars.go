package importer

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// envSet collects compose ${VAR} references lifted into the emitted env:
// struct. Each variable becomes a field the user fills (or a default carried
// over from ${VAR:-default}), so unresolved configuration fails validate
// loudly instead of leaking "${VAR}" text into rendered units.
type envSet struct {
	order []string
	vars  map[string]*envVar
	warnf func(format string, args ...any)
	// resolved: compose-go already interpolated every string (resolve mode).
	// A remaining $ is literal content (e.g. shell syntax in an entrypoint,
	// unescaped from $$), never a compose variable; rewriting it again would
	// lift shell variables into the env struct.
	resolved bool
}

type envVar struct {
	name       string
	def        string
	hasDefault bool
	bare       bool // some occurrence had no default: the field stays required
	warned     bool // mixed bare+default already reported
}

func newEnvSet(warnf func(string, ...any)) *envSet {
	return &envSet{vars: map[string]*envVar{}, warnf: warnf}
}

// varTokenSpans scans s for compose interpolation tokens — $$, $VAR, and
// ${...} — and returns their [start, end) spans. A scanner rather than a
// regexp because compose supports nested interpolation (${A:-${B:-x}}), and a
// pattern stopping at the first '}' would truncate the token and leave a
// stray '}' in the rewritten output. An unterminated ${ is left as literal
// text.
func varTokenSpans(s string) [][2]int {
	var spans [][2]int
	for i := 0; i < len(s); {
		if s[i] != '$' || i+1 >= len(s) {
			i++
			continue
		}
		switch c := s[i+1]; {
		case c == '$':
			spans = append(spans, [2]int{i, i + 2})
			i += 2
		case c == '{':
			depth, j := 0, i+1
			for ; j < len(s); j++ {
				if s[j] == '{' {
					depth++
				} else if s[j] == '}' {
					if depth--; depth == 0 {
						j++
						break
					}
				}
			}
			if depth != 0 {
				i += 2 // unterminated: literal
				continue
			}
			spans = append(spans, [2]int{i, j})
			i = j
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			j := i + 1
			for j < len(s) && (s[j] == '_' || (s[j] >= 'a' && s[j] <= 'z') || (s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			spans = append(spans, [2]int{i, j})
			i = j
		default:
			i++
		}
	}
	return spans
}

// record registers a variable occurrence and returns the env-field reference.
// A bare occurrence keeps the field required even when another occurrence
// carries a default: compose resolves ${TAG:-latest} and a bare ${TAG}
// independently (the bare one to empty when unset), and one CUE field default
// would silently change the bare occurrence's value.
func (e *envSet) record(name, def string, hasDefault bool) string {
	v, ok := e.vars[name]
	if !ok {
		v = &envVar{name: name}
		e.vars[name] = v
		e.order = append(e.order, name)
	}
	if !hasDefault {
		v.bare = true
	} else if !v.hasDefault && !v.bare {
		v.def, v.hasDefault = def, true
	} else if v.hasDefault && v.def != def {
		e.warnf("variable ${%s} has conflicting defaults (%q vs %q); keeping the first", name, v.def, def)
	}
	if v.bare && (v.hasDefault || hasDefault) && !v.warned {
		v.warned = true
		e.warnf("variable ${%s} appears both bare and with a default; compose resolves the bare occurrence to empty when unset, which one CUE field cannot express — treating it as required", name)
		v.hasDefault, v.def = false, ""
	}
	return sel("env", name)
}

// rewrite turns a compose string into a CUE expression: a plain quoted string
// when it contains no variables, else a quoted string with \(env.X)
// interpolations. $$ unescapes to a literal $.
func (e *envSet) rewrite(s string) string {
	if e.resolved || !strings.Contains(s, "$") {
		return strconv.Quote(s)
	}
	var b strings.Builder
	b.WriteByte('"')
	last := 0
	for _, loc := range varTokenSpans(s) {
		b.WriteString(quoteInner(s[last:loc[0]]))
		tok := s[loc[0]:loc[1]]
		last = loc[1]
		if tok == "$$" {
			b.WriteString("$")
			continue
		}
		name, def, hasDefault, alt, nested := parseVarToken(tok)
		if alt {
			e.warnf("variable ${%s} uses :+ (alternate value) which has no CUE equivalent; treating as a required variable", name)
		}
		if nested {
			e.warnf("variable ${%s} has a nested default (%s) that cannot be preserved symbolically; treating as a required variable — use --resolve to bake values instead", name, tok)
			hasDefault, def = false, ""
		}
		b.WriteString(`\(` + e.record(name, def, hasDefault) + `)`)
	}
	b.WriteString(quoteInner(s[last:]))
	b.WriteByte('"')
	return b.String()
}

// parseVarToken decodes an interpolation token (${NAME}, ${NAME:-def},
// ${NAME-def}, ${NAME:?err}, ${NAME:+alt}, $NAME). alt reports the :+ form;
// nested reports a default that itself contains an interpolation
// (${A:-${B:-x}}), which callers cannot carry as a literal.
func parseVarToken(tok string) (name, def string, hasDefault, alt, nested bool) {
	if !strings.HasPrefix(tok, "${") {
		return tok[1:], "", false, false, false
	}
	body := tok[2 : len(tok)-1]
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c == ':' || c == '-' || c == '?' || c == '+' {
			name = body[:i]
			rest := strings.TrimPrefix(body[i:], ":")
			if rest == "" {
				return name, "", false, false, false
			}
			op, arg := rest[0], rest[1:]
			switch op {
			case '-':
				return name, arg, true, false, strings.Contains(arg, "${")
			case '+':
				return name, "", false, true, false
			}
			return name, "", false, false, false
		}
	}
	return body, "", false, false, false
}

// scanRawVariables collects every interpolation variable in the given files,
// reporting whether every occurrence carries a default. One defaulted
// occurrence must not vouch for a bare one elsewhere: with ${TAG:-latest} in
// one place and ${TAG} in another, the latter still resolves to empty when
// TAG is unset. Used in resolve mode to warn about that fallback.
func scanRawVariables(paths []string) map[string]bool {
	found := map[string]bool{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := string(raw)
		var toks []string
		for _, loc := range varTokenSpans(s) {
			toks = append(toks, s[loc[0]:loc[1]])
		}
		// Worklist: a ${...} token's body can itself contain interpolations
		// (${A:-${B}}); B's fallback-to-empty matters just as much as a
		// top-level occurrence's, so nested tokens are scanned too.
		for i := 0; i < len(toks); i++ {
			tok := toks[i]
			if tok == "$$" {
				continue
			}
			if strings.HasPrefix(tok, "${") && strings.HasSuffix(tok, "}") {
				body := tok[2 : len(tok)-1]
				for _, loc := range varTokenSpans(body) {
					toks = append(toks, body[loc[0]:loc[1]])
				}
			}
			name, _, hasDefault, _, _ := parseVarToken(tok)
			if name == "" {
				continue
			}
			if prev, ok := found[name]; ok {
				found[name] = prev && hasDefault
			} else {
				found[name] = hasDefault
			}
		}
	}
	return found
}

// fields returns the env struct fields in first-appearance order.
func (e *envSet) fields() []kv {
	out := make([]kv, 0, len(e.order))
	for _, name := range e.order {
		v := e.vars[name]
		expr := "string"
		if v.hasDefault {
			expr = fmt.Sprintf("string | *%s", strconv.Quote(v.def))
		}
		out = append(out, kv{k: name, v: expr})
	}
	return out
}

// quoteInner escapes a segment for placement inside a CUE double-quoted
// string: strconv.Quote minus the surrounding quotes.
func quoteInner(s string) string {
	q := strconv.Quote(s)
	return q[1 : len(q)-1]
}
