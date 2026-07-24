package eval

import (
	"io/fs"
	"strings"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"

	"github.com/lugoues/creidhne"
)

// constraintHints gives the human explanation for a named schema constraint,
// keyed by the definition name (stable, unlike the regex literal). A definition
// with a hint gets a friendly "not <hint> (#Name)" message when its regex bound
// rejects a value; one without a hint falls through to the raw error. The regex
// itself is NOT kept here: it is derived from the schema (constraintRegexes), so
// editing a pattern in types.cue can never silently desync this table.
var constraintHints = map[string]string{
	"#KeyValue":    `a "key=value" pair`,
	"#ServiceName": "a systemd unit name (*.service, *.target, ...)",
	"#PortMapping": "a port mapping ([ip:]host[:container][/proto])",
	"#UnitName":    "a safe unit name (letters, digits, _ . -)",
	"#Ulimit":      `"name=soft[:hard]" or "host"`,
	"#Signal":      "a signal (SIGTERM, TERM, 9, SIGRTMIN+3)",
	"#MAC":         "a MAC address (aa:bb:cc:dd:ee:ff)",
}

// schemaFilesWithConstraints are the embedded schema files whose definitions
// carry the regex bounds crei translates. types.cue holds nearly all of them;
// images.cue adds the registry ones.
var schemaFilesWithConstraints = []string{"creidhne/types.cue", "creidhne/images.cue"}

var (
	constraintOnce    sync.Once
	constraintRegexes map[string]string // regex literal (as cue prints it) -> def name
)

// constraintTable derives, once, the regex-bound -> definition-name mapping from
// the embedded schema by parsing the source and collecting every `=~"..."` under
// a top-level definition. The literal is stored exactly as it appears in source,
// which is byte-identical to how cue re-prints it in an "out of bound =~..."
// error (escaping included), so a substring match against the error text works.
func constraintTable() map[string]string {
	constraintOnce.Do(func() {
		constraintRegexes = map[string]string{}
		for _, name := range schemaFilesWithConstraints {
			data, err := fs.ReadFile(creidhne.SchemaFS, name)
			if err != nil {
				continue
			}
			f, err := parser.ParseFile(name, data)
			if err != nil {
				continue
			}
			for _, d := range f.Decls {
				fld, ok := d.(*ast.Field)
				if !ok {
					continue
				}
				def, ok := fld.Label.(*ast.Ident)
				if !ok || !strings.HasPrefix(def.Name, "#") {
					continue // only exported definitions
				}
				collectRegexes(fld.Value, def.Name, constraintRegexes)
			}
		}
	})
	return constraintRegexes
}

// collectRegexes walks a definition's value expression, recording the raw text
// of every `=~"..."` bound it finds (descending through &, |, and parens).
func collectRegexes(e ast.Expr, name string, out map[string]string) {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.MAT {
			if lit, ok := x.X.(*ast.BasicLit); ok && lit.Kind == token.STRING && len(lit.Value) >= 2 {
				out[lit.Value[1:len(lit.Value)-1]] = name // strip the surrounding quotes, keep escapes
			}
		}
	case *ast.BinaryExpr:
		collectRegexes(x.X, name, out)
		collectRegexes(x.Y, name, out)
	case *ast.ParenExpr:
		collectRegexes(x.X, name, out)
	}
}
