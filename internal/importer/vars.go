package importer

import (
	"fmt"
	"os"
	"regexp"
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
}

func newEnvSet(warnf func(string, ...any)) *envSet {
	return &envSet{vars: map[string]*envVar{}, warnf: warnf}
}

// varToken matches compose interpolation tokens: $$, ${VAR}, ${VAR<op><arg>},
// and bare $VAR.
var varToken = regexp.MustCompile(`\$(?:\$|\{[^}]+\}|[a-zA-Z_][a-zA-Z0-9_]*)`)

// record registers a variable occurrence and returns the env-field reference.
func (e *envSet) record(name, def string, hasDefault bool) string {
	v, ok := e.vars[name]
	if !ok {
		v = &envVar{name: name, def: def, hasDefault: hasDefault}
		e.vars[name] = v
		e.order = append(e.order, name)
	} else if hasDefault {
		if !v.hasDefault {
			v.def, v.hasDefault = def, true
		} else if v.def != def {
			e.warnf("variable ${%s} has conflicting defaults (%q vs %q); keeping the first", name, v.def, def)
		}
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
	for _, loc := range varToken.FindAllStringIndex(s, -1) {
		b.WriteString(quoteInner(s[last:loc[0]]))
		tok := s[loc[0]:loc[1]]
		last = loc[1]
		if tok == "$$" {
			b.WriteString("$")
			continue
		}
		name, def, hasDefault, alt := parseVarToken(tok)
		if alt {
			e.warnf("variable ${%s} uses :+ (alternate value) which has no CUE equivalent; treating as a required variable", name)
		}
		b.WriteString(`\(` + e.record(name, def, hasDefault) + `)`)
	}
	b.WriteString(quoteInner(s[last:]))
	b.WriteByte('"')
	return b.String()
}

// parseVarToken decodes an interpolation token (${NAME}, ${NAME:-def},
// ${NAME-def}, ${NAME:?err}, ${NAME:+alt}, $NAME). alt reports the :+ form.
func parseVarToken(tok string) (name, def string, hasDefault, alt bool) {
	if !strings.HasPrefix(tok, "${") {
		return tok[1:], "", false, false
	}
	body := tok[2 : len(tok)-1]
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c == ':' || c == '-' || c == '?' || c == '+' {
			name = body[:i]
			rest := strings.TrimPrefix(body[i:], ":")
			if rest == "" {
				return name, "", false, false
			}
			op, arg := rest[0], rest[1:]
			switch op {
			case '-':
				return name, arg, true, false
			case '+':
				return name, "", false, true
			}
			return name, "", false, false
		}
	}
	return body, "", false, false
}

// scanRawVariables collects every interpolation variable in the given files,
// reporting whether each has a default anywhere. Used in resolve mode to warn
// about variables that fall back to empty.
func scanRawVariables(paths []string) map[string]bool {
	found := map[string]bool{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, tok := range varToken.FindAllString(string(raw), -1) {
			if tok == "$$" {
				continue
			}
			name, _, hasDefault, _ := parseVarToken(tok)
			if name == "" {
				continue
			}
			found[name] = found[name] || hasDefault
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
