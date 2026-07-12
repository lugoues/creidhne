// Package importer converts a docker-compose project into a creidhne CUE
// file: one compose project becomes one #Quadlet, services become container
// units, named volumes/networks become volume/network units referenced through
// #self handles, service build sections become build units, and compose
// secrets map onto the #SecretRegistry. Anything that cannot be represented is
// reported, never silently dropped.
package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
)

// Options configures a conversion.
type Options struct {
	// Paths are the compose files (empty: discover compose.yaml and friends
	// in WorkingDir, including override files, like docker compose does).
	Paths []string
	// WorkingDir anchors discovery and relative paths (default: cwd).
	WorkingDir string
	// ProjectName overrides the compose project name (default: from the
	// compose file's name: field, else the working directory name).
	ProjectName string
	// ResolveEnv bakes ${VAR} values at import time instead of lifting them
	// into an env: struct. Values come from EnvFiles and, when UseOsEnv is
	// set, the process environment.
	ResolveEnv bool
	EnvFiles   []string
	UseOsEnv   bool
	// Package is the emitted CUE package name (default "quadlets"). It must
	// match any other .cue files in the destination directory.
	Package string
	// OmitSource skips embedding the source compose file(s) as a trailing
	// comment block in the emitted CUE.
	OmitSource bool
	// PreserveNames emits VolumeName/NetworkName for project-owned resources
	// so an existing compose deployment's volumes and networks are reused
	// (migration). Default off: a fresh import gets fresh systemd-* names.
	// External resources are always adopted by name regardless.
	PreserveNames bool
}

// Result is the emitted CUE plus the conversion report.
type Result struct {
	// QuadletName is the CUE field / #Quadlet name used.
	QuadletName string
	// CUE is the formatted emitted file content.
	CUE []byte
	// Warnings lists everything that did not map faithfully.
	Warnings []string
	// Notes are informational (name preservation, adopted externals).
	Notes []string
	// Steps lists post-import actions (loading secret values, filling env).
	Steps []string
}

// model is the intermediate between compose types and CUE emission.
type model struct {
	quadletName string
	pkg         string
	env         *envSet
	secretKeys  *keymap
	secrets     []secretDecl
	containers  []unitDef
	volumes     []unitDef
	networks    []unitDef
	builds      []unitDef
	// forms is the singular/plural decision made before mapping; emission
	// must use the same decision the reference expressions were built with
	// (an implicitly-declared volume appended mid-mapping must not flip it).
	forms    refs
	sources  []sourceFile
	warnings []string
	notes    []string
	steps    []string
}

// sourceFile is an input compose file embedded (or noted) at the bottom of
// the emitted CUE: label is the user-facing origin (URL or path), content the
// raw bytes, empty when withheld.
type sourceFile struct {
	label   string
	content string
}

type secretDecl struct {
	key      string
	name     string // podman secret name when != key
	external bool
	file     string // compose file: source, for the report
}

// kv is one emitted CUE field: k is the source-side name. Scalar fields carry
// a ready CUE expression in v; list fields carry their elements in items
// (each a CUE expression) and render one element per line.
type kv struct {
	k     string
	v     string
	items []string
}

// section is a quadlet unit section ([Container], [Unit], [Service], ...).
type section struct {
	name   string
	fields []kv
}

// unitDef is one unit of the quadlet. key is the CUE map key (an unquoted
// identifier wherever possible); name carries the compose-side name when it
// differs from key (dashes etc.), emitted as the unit's name: field so stems
// keep the original spelling. comments precede the unit; bodyComments sit at
// the top of its body (dropped compose fields with their original values).
type unitDef struct {
	key          string
	name         string
	comments     []string
	bodyComments []string
	sections     []section
}

func (u *unitDef) section(name string) *section {
	for i := range u.sections {
		if u.sections[i].name == name {
			return &u.sections[i]
		}
	}
	u.sections = append(u.sections, section{name: name})
	return &u.sections[len(u.sections)-1]
}

func (u *unitDef) add(sec, key, expr string) {
	s := u.section(sec)
	s.fields = append(s.fields, kv{k: key, v: expr})
}

// addList appends elements to a list-valued field, merging with an existing
// field of the same key so repeated sources (labels, options) end up in one
// list.
func (u *unitDef) addList(sec, key string, items ...string) {
	if len(items) == 0 {
		return
	}
	s := u.section(sec)
	for i := range s.fields {
		if s.fields[i].k == key && s.fields[i].items != nil {
			s.fields[i].items = append(s.fields[i].items, items...)
			return
		}
	}
	s.fields = append(s.fields, kv{k: key, items: items})
}

// dropComment records a compose field that could not be mapped, keeping its
// original value visible in the emitted unit body.
func (u *unitDef) dropComment(field string, value any) {
	u.bodyComments = append(u.bodyComments, fmt.Sprintf("compose %s: %v (not mapped; see conversion report)", field, value))
}

func (m *model) warnf(format string, args ...any) {
	m.warnings = append(m.warnings, fmt.Sprintf(format, args...))
}

func (m *model) stepf(format string, args ...any) {
	m.steps = append(m.steps, fmt.Sprintf(format, args...))
}

func (m *model) notef(format string, args ...any) {
	m.notes = append(m.notes, fmt.Sprintf(format, args...))
}

// keymap assigns each compose name a CUE map key: the name itself when it is
// already an identifier, else a sanitized identifier (dashes and dots become
// underscores) with the original spelling preserved via the unit's name:
// field. A sanitized key that collides or still isn't an identifier falls
// back to the original name (emitted quoted, referenced via brackets).
type keymap struct {
	byName map[string]string
	used   map[string]bool
}

func newKeymap() *keymap {
	return &keymap{byName: map[string]string{}, used: map[string]bool{}}
}

func (k *keymap) key(name string) string {
	if key, ok := k.byName[name]; ok {
		return key
	}
	key := name
	if !identRe.MatchString(key) {
		cand := strings.NewReplacer("-", "_", ".", "_").Replace(name)
		if identRe.MatchString(cand) && !k.used[cand] {
			key = cand
		}
	}
	k.byName[name] = key
	k.used[key] = true
	return key
}

// refs renders cross-unit reference expressions: singular or plural form per
// kind, with plural keys resolved through the kind's keymap.
type refs struct {
	singularContainer bool
	singularVolume    bool
	singularNetwork   bool
	singularBuild     bool

	containers *keymap
	volumes    *keymap
	networks   *keymap
	builds     *keymap
}

var identRe = regexp.MustCompile(`^[a-zA-Z_$][a-zA-Z0-9_$]*$`)

// sel renders a CUE member access for a possibly non-identifier key.
func sel(base, key string) string {
	if identRe.MatchString(key) {
		return base + "." + key
	}
	return base + "[" + strconv.Quote(key) + "]"
}

// cueKey renders a struct field label.
func cueKey(k string) string {
	if identRe.MatchString(k) {
		return k
	}
	return strconv.Quote(k)
}

func (r refs) containerService(name string) string {
	if r.singularContainer {
		return "units.#container.#service"
	}
	return sel("units.containers", r.containers.key(name)) + ".#service"
}

func (r refs) containerSelf(name string) string {
	if r.singularContainer {
		return "units.#container.#self"
	}
	return sel("units.containers", r.containers.key(name)) + ".#self"
}

func (r refs) volumeSelf(name string) string {
	if r.singularVolume {
		return "units.#volume.#self"
	}
	return sel("units.volumes", r.volumes.key(name)) + ".#self"
}

func (r refs) networkSelf(name string) string {
	if r.singularNetwork {
		return "units.#network.#self"
	}
	return sel("units.networks", r.networks.key(name)) + ".#self"
}

func (r refs) buildSelf(name string) string {
	if r.singularBuild {
		return "units.#build.#self"
	}
	return sel("units.builds", r.builds.key(name)) + ".#self"
}

// newUnit builds a unitDef keyed through the kind's keymap, setting the name:
// field when the key had to diverge from the compose name.
func newUnit(km *keymap, name string) unitDef {
	key := km.key(name)
	u := unitDef{key: key}
	if key != name {
		u.name = name
	}
	return u
}

// collectSources gathers the compose files for verbatim embedding at the
// bottom of the emitted CUE (instructional comments in the source survive
// that way). Labels map temp downloads back to their original URLs. A project
// carrying inline secret content is not embedded: we refuse to copy secret
// material into CUE, and a comment block is still CUE. Env files are never
// embedded for the same reason.
func collectSources(project *types.Project, opts Options, labels map[string]string, notef func(string, ...any)) []sourceFile {
	if opts.OmitSource {
		return nil
	}
	inlineSecret := ""
	for key, s := range project.Secrets {
		if s.Content != "" {
			inlineSecret = key
			break
		}
	}
	var out []sourceFile
	for _, f := range project.ComposeFiles {
		label := f
		if orig, ok := labels[f]; ok {
			label = orig
		} else if wd := opts.WorkingDir; wd != "" {
			if rel, err := filepath.Rel(wd, f); err == nil && !strings.HasPrefix(rel, "..") {
				label = rel
			}
		}
		if inlineSecret != "" {
			notef("source %s not embedded: secrets.%s carries inline content, which does not belong in CUE", label, inlineSecret)
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			notef("source %s not embedded: %v", label, err)
			continue
		}
		out = append(out, sourceFile{label: label, content: strings.ReplaceAll(string(raw), "\r\n", "\n")})
	}
	return out
}

// Convert loads the compose project and produces the CUE file and report.
// Paths may be http(s) URLs (e.g. a GitHub file link): they are fetched to a
// temp dir first, with browser blob URLs rewritten to their raw form.
func Convert(opts Options) (*Result, error) {
	var preWarnings []string
	opts, sourceLabels, cleanup, err := resolveRemotePaths(opts, func(f string, a ...any) {
		preWarnings = append(preWarnings, fmt.Sprintf(f, a...))
	})
	if err != nil {
		return nil, err
	}
	defer cleanup()
	// compose-go absolutizes config paths; label sources by the user's own
	// spelling so the embed header is stable and readable.
	for _, p := range opts.Paths {
		if abs, err := filepath.Abs(p); err == nil {
			if _, exists := sourceLabels[abs]; !exists {
				sourceLabels[abs] = p
			}
		}
	}
	if opts.ResolveEnv {
		// Baking values: variables with no default anywhere resolve from
		// --env-file/.env/the environment, or silently to empty. Name them.
		var empties []string
		for name, hasDefault := range scanRawVariables(opts.Paths) {
			if !hasDefault {
				empties = append(empties, name)
			}
		}
		if len(empties) > 0 {
			sort.Strings(empties)
			preWarnings = append(preWarnings, fmt.Sprintf(
				"variables without defaults resolved from --env-file/.env/the environment, or to empty if unset: %s", strings.Join(empties, ", ")))
		}
	}
	project, err := loadCompose(opts)
	if err != nil {
		return nil, err
	}
	m, err := mapProject(project, opts)
	if err != nil {
		return nil, err
	}
	m.sources = collectSources(project, opts, sourceLabels, m.notef)
	m.pkg = opts.Package
	if m.pkg == "" {
		m.pkg = "quadlets"
	}
	out, err := emit(m)
	if err != nil {
		return nil, err
	}
	m.warnings = append(m.warnings, preWarnings...)
	sort.Strings(m.warnings)
	return &Result{
		QuadletName: m.quadletName,
		CUE:         out,
		Warnings:    m.warnings,
		Notes:       m.notes,
		Steps:       m.steps,
	}, nil
}
