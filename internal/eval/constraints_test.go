package eval

import "testing"

// TestConstraintTableDerivesFromSchema: the regex->name table is built from the
// embedded schema, and every hinted definition still appears in it. If a regex
// is edited or a definition renamed in types.cue, this fails loudly instead of
// letting the error translation silently go stale.
func TestConstraintTableDerivesFromSchema(t *testing.T) {
	table := constraintTable()
	if len(table) == 0 {
		t.Fatal("derived no constraints from the embedded schema")
	}

	// Every definition we keep a hint for must still declare a regex bound.
	haveDef := map[string]bool{}
	for _, def := range table {
		haveDef[def] = true
	}
	for def := range constraintHints {
		if !haveDef[def] {
			t.Errorf("%s has a hint but no regex bound in the schema (renamed or its =~ removed?)", def)
		}
	}

	// Spot-check a couple of exact mappings so a wholesale mis-parse is caught.
	if table[`^[^=]+=.*$`] != "#KeyValue" {
		t.Errorf("#KeyValue regex not mapped; got %q", table[`^[^=]+=.*$`])
	}
	if got := table[`^(SIG)?([A-Z]+([+-][0-9]+)?|[0-9]+)$`]; got != "#Signal" {
		t.Errorf("#Signal regex not mapped; got %q", got)
	}
}

// TestConstraintForMatchesDerived: constraintFor names a definition from a real
// "out of bound" message, proving the derived literal matches cue's escaped
// error text (the escaping is why the literal is kept, not unquoted).
func TestConstraintForMatchesDerived(t *testing.T) {
	// A MAC bound with a backslash-free regex, and #Signal.
	cases := []struct{ msg, want string }{
		{`invalid value "x" (out of bound =~"^[^=]+=.*$")`, "#KeyValue"},
		{`invalid value "nope" (out of bound =~"^(SIG)?([A-Z]+([+-][0-9]+)?|[0-9]+)$")`, "#Signal"},
	}
	for _, c := range cases {
		name, hint, ok := constraintFor(c.msg)
		if !ok || name != c.want || hint == "" {
			t.Errorf("constraintFor(%q) = %q,%q,%v; want %s with a hint", c.msg, name, hint, ok, c.want)
		}
	}
}
