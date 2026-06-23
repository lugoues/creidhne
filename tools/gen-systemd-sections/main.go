// Command gen-systemd-sections generates CUE schema and Go-template partials for
// systemd's pass-through unit-file sections ([Unit]/[Service]/[Install]) from
// systemd's own parser table (load-fragment-gperf.gperf.in, vendored & pinned),
// so the schema tracks systemd rather than being hand-maintained.
//
// The gperf table is ground truth for which directives are valid on disk and in
// which section; each directive's config_parse_* function gives its base type.
// [Service] additionally pulls in the EXEC/CGROUP/KILL context directives, which
// the gperf defines once as Jinja macros and invokes per section; collectDirective-
// Lines expands those invocations (substituting the {{type}} placeholder).
//
// Enum *values* aren't in this table (they live in DEFINE_STRING_TABLE_LOOKUP
// macros), so enum-typed directives map to a curated CUE type (e.g. #JobMode,
// #EmergencyAction, #ServiceType) defined by hand in types.cue. Parsers with no
// mapping fall back to string (a safe pass-through) and are reported to stderr.
//
// Run from the repo root: `go run ./tools/gen-systemd-sections`.
// It writes creidhne/systemd_sections.gen.cue and templates/systemd_sections.gen.tpl.
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
	{"Service", "#ServiceSection", "service", true},
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

	// --- [Service]: systemd.service/exec/resource-control/kill ---
	// durations
	"config_parse_sec_fix_0":             "#TimeSpan",
	"config_parse_sec_def_infinity":      "#TimeSpan",
	"config_parse_service_timeout":       "#TimeSpan",
	"config_parse_service_timeout_abort": "#TimeSpan",
	// numeric / resource controls
	"config_parse_rlimit":        "#ResourceLimit", // byte-valued ones overridden to #ByteLimit
	"config_parse_memory_limit":  "#Bytes",
	"config_parse_cpu_quota":     "#Percent",
	"config_parse_cg_cpu_weight": "#CPUWeight",
	"config_parse_cg_weight":     "#IOWeight",
	"config_parse_tasks_max":     "#TasksLimit",
	"config_parse_mode":          "#FileMode",
	"config_parse_signal":        "#Signal",
	// service enums (values curated in types.cue)
	"config_parse_service_type":    "#ServiceType",
	"config_parse_service_restart": "#ServiceRestart",
	"config_parse_kill_mode":       "#KillMode",
	"config_parse_notify_access":   "#NotifyAccess",
	// string lists (repeatable / space-separated)
	"config_parse_exec":                        "[...string]",
	"config_parse_namespace_path_strv":         "[...string]",
	"config_parse_exec_directories":            "[...string]",
	"config_parse_bind_paths":                  "[...string]",
	"config_parse_temporary_filesystems":       "[...string]",
	"config_parse_set_credential":              "[...string]",
	"config_parse_load_credential":             "[...string]",
	"config_parse_set_status":                  "[...string]",
	"config_parse_syscall_filter":              "[...string]",
	"config_parse_syscall_log":                 "[...string]",
	"config_parse_syscall_archs":               "[...string]",
	"config_parse_capability_set":              "[...string]",
	"config_parse_in_addr_prefixes":            "[...string]",
	"config_parse_unset_environ":               "[...string]",
	"config_parse_environ":                     "[...string]",
	"config_parse_unit_env_file":               "[...string]",
	"config_parse_user_group_strv_compat":      "[...string]",
	"config_parse_restrict_network_interfaces": "[...string]",
	"config_parse_restrict_filesystems":        "[...string]",
	"config_parse_ip_filter_bpf_progs":         "[...string]",
	"config_parse_io_limit":                    "[...string]",
	"config_parse_blockio_bandwidth":           "[...string]",
	"config_parse_blockio_weight":              "[...string]",
	"config_parse_cgroup_socket_bind":          "[...string]",
	"config_parse_log_extra_fields":            "[...string]",
	"config_parse_open_file":                   "[...string]",
	"config_parse_nft_set":                     "[...string]",
	"config_parse_address_families":            "[...string]",
	"config_parse_blockio_device_weight":       "[...string]",
	"config_parse_bpf_foreign_program":         "[...string]",
	"config_parse_cgroup_nft_set":              "[...string]",
	"config_parse_device_allow":                "[...string]",
	"config_parse_disable_controllers":         "[...string]",
	"config_parse_exec_secure_bits":            "[...string]",
	"config_parse_extension_images":            "[...string]",
	"config_parse_import_credential":           "[...string]",
	"config_parse_io_device_latency":           "[...string]",
	"config_parse_io_device_weight":            "[...string]",
	"config_parse_log_filter_patterns":         "[...string]",
	"config_parse_mount_images":                "[...string]",
	"config_parse_pass_environ":                "[...string]",
	"config_parse_restrict_namespaces":         "[...string]",
	"config_parse_service_sockets":             "[...string]",
	"config_parse_nsec":                        "#TimeSpan",
	// plain strings
	"config_parse_exec_output":        "string", // Standard{Output,Error}: journal|file:PATH|fd:NAME|...
	"config_parse_working_directory":  "string",
	"config_parse_unit_slice":         "string",
	"config_parse_image_policy":       "string",
	"config_parse_root_image_options": "string",
	"config_parse_user_group_compat":  "string",
	"config_parse_pid_file":           "string",

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

	// Byte-valued rlimits (config_parse_rlimit maps to #ResourceLimit by default,
	// which rejects byte suffixes); these accept sizes like "512M".
	"Service.LimitFSIZE":    "#ByteLimit",
	"Service.LimitDATA":     "#ByteLimit",
	"Service.LimitSTACK":    "#ByteLimit",
	"Service.LimitCORE":     "#ByteLimit",
	"Service.LimitRSS":      "#ByteLimit",
	"Service.LimitAS":       "#ByteLimit",
	"Service.LimitMEMLOCK":  "#ByteLimit",
	"Service.LimitMSGQUEUE": "#ByteLimit",
}

// legacyAliases are directives the generator drops because a canonical form is
// already emitted. Two kinds, both keyed "Section.Directive":
//  1. Legacy *spellings* whose canonical name lives in the same section
//     (e.g. Unit.BindTo -> BindsTo; Service.ReadWriteDirectories -> ReadWritePaths).
//  2. Legacy *locations*: [Unit] directives systemd still accepts under [Service]
//     for backwards compat. The canonical [Unit] entry is generated into
//     #UnitSection, so the redundant [Service] copy is omitted.
//
// This set is deliberately explicit rather than inferred from the parser/target
// columns: many *distinct* directives legitimately share a generic context-struct
// offset (e.g. every Memory*/IO* cgroup directive targets offsetof(_, cgroup_context)
// and is dispatched by name), so deduping on shared targets silently drops real
// directives. Worst case here is a missed alias surviving as an extra valid field;
// it can never drop a real directive.
var legacyAliases = map[string]bool{
	// [Unit] legacy spellings (canonical in [Unit])
	"Unit.BindTo":              true, // -> BindsTo
	"Unit.PropagateReloadTo":   true, // -> PropagatesReloadTo
	"Unit.PropagateReloadFrom": true, // -> ReloadPropagatedFrom
	"Unit.StartLimitInterval":  true, // -> StartLimitIntervalSec

	// [Service] legacy spellings (canonical in [Service])
	"Service.ReadWriteDirectories":    true, // -> ReadWritePaths
	"Service.ReadOnlyDirectories":     true, // -> ReadOnlyPaths
	"Service.InaccessibleDirectories": true, // -> InaccessiblePaths

	// [Service] legacy locations (canonical in [Unit], emitted into #UnitSection)
	"Service.FailureAction":      true,
	"Service.RebootArgument":     true,
	"Service.StartLimitAction":   true,
	"Service.StartLimitBurst":    true,
	"Service.StartLimitInterval": true,
}

type directive struct {
	name, cueType string
}

func renderKind(t string) string {
	switch t {
	case "[...string]":
		return "list"
	case "uint", "int":
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

	numVer := strings.TrimPrefix(version, "v")
	docs, err := loadDocs(version, numVer)
	if err != nil {
		die(err)
	}

	writeCUE(version, numVer, parsed, docs)
	writeTemplate(version, parsed)

	var undoc []string
	for _, ds := range parsed {
		for _, d := range ds {
			if _, ok := docs[d.name]; !ok {
				undoc = append(undoc, d.name)
			}
		}
	}
	if len(undoc) > 0 {
		sort.Strings(undoc)
		fmt.Fprintf(os.Stderr, "NOTE: %d directive(s) had no man-page doc: %s\n", len(undoc), strings.Join(undoc, ", "))
	}

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

// parseMacros collects the body directive lines of each
// {%- macro NAME(type) -%} ... {%- endmacro -%} block, keyed by NAME. Bodies use
// the literal "{{type}}." prefix, substituted per invocation later.
func parseMacros(lines []string) map[string][]string {
	macros := map[string][]string{}
	cur := ""
	for _, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case strings.Contains(t, "{%- macro "):
			s := t[strings.Index(t, "macro ")+len("macro "):]
			cur = strings.TrimSpace(s[:strings.Index(s, "(")])
		case strings.Contains(t, "{%- endmacro"):
			cur = ""
		case cur != "" && strings.HasPrefix(t, "{{type}}."):
			macros[cur] = append(macros[cur], t)
		}
	}
	return macros
}

// collectDirectiveLines returns the directive lines for a section: its literal
// "Section.*" lines plus, for each "{{ MACRO('Section') }}" invocation, that
// macro's body with {{type}} substituted. Lines inside macro definitions are
// skipped (they're picked up via invocations).
func collectDirectiveLines(lines []string, section string) []string {
	macros := parseMacros(lines)
	var out []string
	inDef := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case strings.Contains(t, "{%- macro "):
			inDef = true
		case strings.Contains(t, "{%- endmacro"):
			inDef = false
		case inDef:
			// skip macro-definition bodies
		case strings.HasPrefix(t, section+"."):
			out = append(out, t)
		case strings.HasPrefix(t, "{{") && strings.Contains(t, "('"+section+"')"):
			name := strings.TrimSpace(strings.TrimPrefix(t, "{{"))
			name = strings.TrimSpace(name[:strings.Index(name, "(")])
			for _, bl := range macros[name] {
				out = append(out, strings.ReplaceAll(bl, "{{type}}", section))
			}
		}
	}
	return out
}

// parseSection extracts a section's directives (expanding context macros),
// skipping deprecated parsers and the explicit legacyAliases set. It does NOT
// dedup on shared parser/target columns: many distinct directives legitimately
// share a generic context-struct offset (see legacyAliases), so target dedup
// would silently drop real directives.
func parseSection(lines []string, section string, unmapped map[string]bool) []directive {
	prefix := section + "."
	var out []directive
	seenName := map[string]bool{}
	for _, line := range collectDirectiveLines(lines, section) {
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(fields[0], prefix))
		parser := strings.TrimSpace(fields[1])
		if name == "" || seenName[name] || legacyAliases[section+"."+name] {
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
		seenName[name] = true
		out = append(out, directive{name, t})
	}
	return out
}

func writeCUE(version, numVer string, parsed map[string][]directive, docs map[string]docEntry) {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by tools/gen-systemd-sections; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: systemd %s, load-fragment-gperf.gperf.in + man pages.\n", version)
	fmt.Fprintf(&b, "package creidhne\n")
	for _, s := range sections {
		core, cond, asrt := group(parsed[s.gperf])
		fmt.Fprintf(&b, "\n%s: {\n", s.cueDef)
		emitCUE(&b, "", core, docs, numVer)
		emitCUE(&b, "Condition* (systemd.unit(5))", cond, docs, numVer)
		emitCUE(&b, "Assert* (systemd.unit(5))", asrt, docs, numVer)
		fmt.Fprintf(&b, "}\n")
	}
	if err := os.WriteFile(cueOut, []byte(b.String()), 0o644); err != nil {
		die(err)
	}
}

func emitCUE(b *strings.Builder, title string, ds []directive, docs map[string]docEntry, numVer string) {
	if len(ds) == 0 {
		return
	}
	if title != "" {
		// Trailing blank line so the title is its own (detached) comment, not the
		// first line of the next field's doc comment.
		fmt.Fprintf(b, "\n\t// %s\n\n", title)
	}
	for i, d := range ds {
		if i > 0 {
			fmt.Fprintf(b, "\n") // blank line between entries: keeps each field's doc its own block
		}
		if doc, ok := docs[d.name]; ok {
			emitDoc(b, doc, numVer)
		}
		fmt.Fprintf(b, "\t%s?: %s\n", d.name, d.cueType)
	}
}

// emitDoc writes a directive's documentation as CUE line comments: each
// description paragraph separated by a blank comment line, then a link to the man
// page. No directive-name header — the CUE field key sits directly below the
// comment, and a shared entry (e.g. the 30+ Assert*/Condition* directives) would
// otherwise repeat every sibling name on every field. The link anchor still
// carries the canonical group target. CUE concatenates the lines into one doc
// comment; VS Code renders the markdown (paragraphs + clickable link).
func emitDoc(b *strings.Builder, doc docEntry, numVer string) {
	link := fmt.Sprintf("https://www.freedesktop.org/software/systemd/man/%s/%s.html#%s", numVer, doc.page, doc.anchor)
	parts := append(append([]string{}, doc.paras...), link)
	for i, part := range parts {
		if i > 0 {
			b.WriteString("\t//\n")
		}
		fmt.Fprintf(b, "\t// %s\n", part)
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

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
