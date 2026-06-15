// Command gen-systemd-sections generates CUE schema and Go-template partials for
// systemd's pass-through unit-file sections ([Unit]/[Install]) from systemd's own
// parser table (load-fragment-gperf.gperf.in, vendored & pinned), so the schema
// tracks systemd rather than being hand-maintained.
//
// The gperf table is ground truth for which directives are valid on disk and in
// which section; each directive's config_parse_* function gives its base type.
// Enum *values* aren't in this table (they live in DEFINE_STRING_TABLE_LOOKUP
// macros), so enum-typed directives map to a curated CUE type (e.g. #JobMode,
// #EmergencyAction) defined by hand in types.cue.
//
// Run from the repo root: `go run ./tools/gen-systemd-sections`.
// It writes creidhne/systemd_sections.gen.cue and templates/systemd_sections.gen.tpl.
//
// [Service] is not yet handled — it additionally needs the EXEC/CGROUP/KILL
// context-macro expansion.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	gperfPath   = "tools/gen-systemd-sections/load-fragment-gperf.gperf.in"
	versionPath = "tools/gen-systemd-sections/SYSTEMD_VERSION"
	cueOut      = "creidhne/systemd_sections.gen.cue"
	tplOut      = "templates/systemd_sections.gen.tpl"
)

// sections to generate: gperf section name -> (CUE def, template partial name,
// template data path).
var sections = []struct {
	gperf, cueDef, partial string
	leadingBlank           bool // blank line before the header (section separator)
}{
	{"Unit", "#UnitSection", "unit", false}, // first section: no leading blank
	{"Install", "#InstallSection", "install", true},
}

// cueType maps a config_parse_* function to a CUE type. Unmapped parsers fall
// back to string (and are reported). "" means skip (deprecated/obsolete).
var cueType = map[string]string{
	"config_parse_bool":             "bool",
	"config_parse_tristate":         "bool",
	"config_parse_job_mode_isolate": "bool",

	"config_parse_sec":                     "#TimeSpan",
	"config_parse_job_timeout_sec":         "#TimeSpan",
	"config_parse_job_running_timeout_sec": "#TimeSpan",

	"config_parse_unsigned": "uint",

	"config_parse_unit_deps":       "[...string]",
	"config_parse_documentation":   "[...string]",
	"config_parse_unit_mounts_for": "[...string]",
	"config_parse_strv":            "[...string]",
	"config_parse_install_strv":    "[...string]", // Install.Also/WantedBy/... (alias resolves below)

	"config_parse_unit_string_printf": "string",
	"config_parse_unit_path_printf":   "string",
	"config_parse_string":             "string",
	"config_parse_reboot_parameter":   "string",
	"config_parse_exit_status":        "string",

	"config_parse_unit_condition_string": "[...string]",
	"config_parse_unit_condition_path":   "[...string]",

	// enums (values curated in types.cue)
	"config_parse_job_mode":         "#JobMode",
	"config_parse_emergency_action": "#EmergencyAction",
	"config_parse_collect_mode":     "#CollectMode",

	// skipped
	"config_parse_warn_compat":        "",
	"config_parse_obsolete_unit_deps": "",
}

// overrides set the type for "Section.Directive" entries the parser column can't
// classify, notably [Install], whose directives use a NULL parser (systemd
// parses that section specially).
var overrides = map[string]string{
	"Install.Alias":           "[...string]",
	"Install.WantedBy":        "[...string]",
	"Install.RequiredBy":      "[...string]",
	"Install.UpheldBy":        "[...string]",
	"Install.Also":            "[...string]",
	"Install.DefaultInstance": "string",
}

type directive struct {
	name, cueType string
}

func renderKind(t string) string {
	switch {
	case t == "[...string]":
		return "list"
	case t == "uint" || t == "int":
		return "int"
	default:
		return "scalar"
	}
}

func main() {
	raw, err := os.ReadFile(gperfPath)
	if err != nil {
		die(err)
	}
	version := "unknown"
	if b, err := os.ReadFile(versionPath); err == nil {
		version = strings.TrimSpace(string(b))
	}
	lines := strings.Split(string(raw), "\n")

	unmapped := map[string]bool{}
	parsed := map[string][]directive{} // gperf section -> directives
	for _, s := range sections {
		parsed[s.gperf] = parseSection(lines, s.gperf, unmapped)
	}

	writeCUE(version, parsed)
	writeTemplate(version, parsed)

	for s, ds := range parsed {
		fmt.Fprintf(os.Stderr, "[%s]: %d directives\n", s, len(ds))
	}
	if len(unmapped) > 0 {
		var names []string
		for p := range unmapped {
			names = append(names, p)
		}
		sort.Strings(names)
		fmt.Fprintf(os.Stderr, "NOTE: %d parser(s) fell back to string: %s\n", len(names), strings.Join(names, ", "))
	}
	fmt.Fprintf(os.Stderr, "wrote %s and %s\n", cueOut, tplOut)
}

// parseSection extracts a section's directives, skipping deprecated parsers and
// deduplicating aliases (directives sharing a parser + target spec — e.g.
// BindTo/BindsTo — keep the first/canonical spelling).
func parseSection(lines []string, section string, unmapped map[string]bool) []directive {
	prefix := section + "."
	var out []directive
	seenName := map[string]bool{}
	seenTarget := map[string]bool{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(fields[0], prefix))
		parser := strings.TrimSpace(fields[1])
		if name == "" || seenName[name] {
			continue
		}
		var t string
		if o, ok := overrides[section+"."+name]; ok {
			t = o
		} else {
			var mapped bool
			t, mapped = cueType[parser]
			if mapped && t == "" {
				continue // skipped (deprecated/obsolete)
			}
			if !mapped {
				t = "string"
				unmapped[parser] = true
			}
		}
		// Alias dedup: directives sharing a parser + a *non-trivial* target spec
		// (e.g. BindTo/BindsTo -> UNIT_BINDS_TO) are the same setting under a
		// legacy name; keep the first (canonical) one. A trivial target ("0, 0",
		// as in [Install]) means the parser dispatches by name, so don't dedup.
		rest := strings.Join(fields[2:], ",")
		if !trivialTarget(rest) {
			target := parser + "|" + rest
			if seenTarget[target] {
				continue
			}
			seenTarget[target] = true
		}
		seenName[name] = true
		out = append(out, directive{name, t})
	}
	return out
}

func writeCUE(version string, parsed map[string][]directive) {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by tools/gen-systemd-sections; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: systemd %s, load-fragment-gperf.gperf.in.\n", version)
	fmt.Fprintf(&b, "package creidhne\n")
	for _, s := range sections {
		core, cond, asrt := group(parsed[s.gperf])
		fmt.Fprintf(&b, "\n%s: {\n", s.cueDef)
		emitCUE(&b, "", core)
		emitCUE(&b, "Condition* (systemd.unit(5))", cond)
		emitCUE(&b, "Assert* (systemd.unit(5))", asrt)
		fmt.Fprintf(&b, "}\n")
	}
	if err := os.WriteFile(cueOut, []byte(b.String()), 0o644); err != nil {
		die(err)
	}
}

func emitCUE(b *strings.Builder, title string, ds []directive) {
	if len(ds) == 0 {
		return
	}
	if title != "" {
		fmt.Fprintf(b, "\n\t// %s\n", title)
	}
	for _, d := range ds {
		fmt.Fprintf(b, "\t%s?: %s\n", d.name, d.cueType)
	}
}

func writeTemplate(version string, parsed map[string][]directive) {
	var b strings.Builder
	fmt.Fprintf(&b, "{{- /* Code generated by tools/gen-systemd-sections from systemd %s; DO NOT EDIT. */ -}}\n", version)
	for _, s := range sections {
		header := "[" + s.gperf + "]"
		ifClause := "{{ if . -}}" // no leading blank line
		if s.leadingBlank {
			ifClause = "{{ if . }}" // keep the newline => blank line before header
		}
		fmt.Fprintf(&b, "{{- define %q -}}\n%s\n%s\n", s.partial, ifClause, header)
		all := parsed[s.gperf]
		for _, d := range all {
			switch renderKind(d.cueType) {
			case "list":
				fmt.Fprintf(&b, "{{ range .%s -}}%s={{ . }}\n{{ end -}}\n", d.name, d.name)
			case "int":
				fmt.Fprintf(&b, "{{ if isset . %q -}}%s={{ printf \"%%d\" .%s }}\n{{ end -}}\n", d.name, d.name, d.name)
			default:
				fmt.Fprintf(&b, "{{ if isset . %q -}}%s={{ .%s }}\n{{ end -}}\n", d.name, d.name, d.name)
			}
		}
		fmt.Fprintf(&b, "{{ end -}}\n{{- end -}}\n")
	}
	if err := os.WriteFile(tplOut, []byte(b.String()), 0o644); err != nil {
		die(err)
	}
}

// group splits directives into core / Condition* / Assert*, sorting the families.
func group(ds []directive) (core, cond, asrt []directive) {
	for _, d := range ds {
		switch {
		case strings.HasPrefix(d.name, "Condition"):
			cond = append(cond, d)
		case strings.HasPrefix(d.name, "Assert"):
			asrt = append(asrt, d)
		default:
			core = append(core, d)
		}
	}
	sort.Slice(cond, func(i, j int) bool { return cond[i].name < cond[j].name })
	sort.Slice(asrt, func(i, j int) bool { return asrt[i].name < asrt[j].name })
	return
}

// trivialTarget reports whether a gperf target spec is just zeros (no real
// struct offset or enum tag), meaning the parser dispatches by directive name.
func trivialTarget(rest string) bool {
	r := strings.NewReplacer("0", "", ",", "", " ", "", "\t", "")
	return r.Replace(rest) == ""
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
