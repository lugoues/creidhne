// Package render turns evaluated unit data into the final Quadlet unit files by
// executing the embedded Go text/templates. Templating moved out of CUE into Go
// so the CUE->Go boundary is plain data; the .tpl files are unchanged.
package render

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"text/template"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/kinds"
)

// ensureLocal rejects any output path that is not a relative path contained
// within the quadlet directory. A unit name or build-context key carrying ".."
// or an absolute path would otherwise be joined onto the quadlet dir at apply
// time and write outside it. The CUE schema constrains unit names too; this is
// the defense-in-depth layer that also covers build-context keys (which are
// legitimately path-shaped) and any caller that bypasses the schema.
func ensureLocal(name string) error {
	if !filepath.IsLocal(filepath.FromSlash(name)) {
		return fmt.Errorf("refusing unsafe output path %q: must be a relative path inside the quadlet directory", name)
	}
	return nil
}

// FileContent is a rendered file plus an optional octal mode (set for build
// context files like executable scripts; empty means default permissions).
type FileContent struct {
	Content []byte
	Mode    string
}

// Renderer holds the parsed templates, one per unit kind.
type Renderer struct {
	tmpl *template.Template // all .tpl files in one set, so shared partials resolve
}

// funcMap exposes helpers to the templates. isset reports whether a key is
// present in a section map, regardless of its value, so a field set to a falsy
// value (e.g. an integer 0 or a boolean false) still renders, unlike the plain
// `{{ if .Section.Field }}` truthiness check which silently drops it.
var funcMap = template.FuncMap{
	"isset": func(m map[string]any, key string) bool {
		if m == nil {
			return false
		}
		_, ok := m[key]
		return ok
	},
}

// New parses every *.tpl from tplFS into one template set (expected at its root,
// e.g. fs.Sub(embeddedFS, "templates") or os.DirFS("templates")). Parsing them
// together lets the per-kind templates invoke shared partials (e.g. the
// generated "unit"/"install" section partials).
func New(tplFS fs.FS) (*Renderer, error) {
	// Note: missingkey=error is deliberately NOT set. The templates access
	// optional top-level sections (.Unit, .Service, .Install, .Quadlet) directly
	// and rely on a missing section rendering as absent; missingkey=error would
	// turn every unset optional section into a render error. The silent-<no
	// value> risk for unguarded direct-print fields is instead covered upstream
	// by eval's cue.Concrete validation, which rejects incomplete unit data.
	tmpl, err := template.New("creidhne").Funcs(funcMap).ParseFS(tplFS, "*.tpl")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	for _, kind := range kinds.Kinds() {
		if tmpl.Lookup(kind+".tpl") == nil {
			return nil, fmt.Errorf("missing template %q.tpl", kind)
		}
	}
	return &Renderer{tmpl: tmpl}, nil
}

// BuildFileSet renders every unit across all quadlets into a filename->content
// map, including build artifacts (images/<stem>.Containerfile and
// images/<stem>.context/<path>).
func (r *Renderer) BuildFileSet(quadlets []eval.Quadlet) (map[string]FileContent, error) {
	files := make(map[string]FileContent)
	// owners tracks which quadlet produced each filename so a collision (two
	// units resolving to the same file) is a hard error rather than a silent
	// last-writer-wins overwrite. The CUE `files:` struct used to enforce this
	// via unification conflicts.
	owners := make(map[string]string)
	for _, q := range quadlets {
		for _, u := range q.Units {
			if err := ensureLocal(u.Filename); err != nil {
				return nil, fmt.Errorf("quadlet %q: %w", q.Name, err)
			}
			if prev, ok := owners[u.Filename]; ok {
				return nil, fmt.Errorf("duplicate output file %q: emitted by both quadlet %q and quadlet %q", u.Filename, prev, q.Name)
			}
			owners[u.Filename] = q.Name
			content, err := r.renderUnit(u)
			if err != nil {
				return nil, fmt.Errorf("render %s: %w", u.Filename, err)
			}
			files[u.Filename] = FileContent{Content: content}
			if u.Kind == "build" {
				if err := addBuildArtifacts(files, owners, q.Name, u); err != nil {
					return nil, fmt.Errorf("build artifacts for %s: %w", u.Stem, err)
				}
			}
		}
	}
	return files, nil
}

// renderUnit executes the template for one unit. The template root is the unit
// data plus, for builds with an inline Containerfile, the injected
// containerfilePath/contextPath the prototype's render.cue used to supply.
func (r *Renderer) renderUnit(u eval.UnitRecord) ([]byte, error) {
	name := u.Kind + ".tpl"
	if r.tmpl.Lookup(name) == nil {
		return nil, fmt.Errorf("unknown unit kind %q", u.Kind)
	}
	root := make(map[string]any, len(u.Data)+2)
	for k, v := range u.Data {
		root[k] = v
	}
	if u.Kind == "build" {
		if _, ok := u.Data["ContainerFile"]; ok {
			root["containerfilePath"] = "images/" + u.Stem + ".Containerfile"
			if _, ok := u.Data["Context"]; ok {
				root["contextPath"] = "images/" + u.Stem + ".context"
			}
		}
	}
	var buf bytes.Buffer
	if err := r.tmpl.ExecuteTemplate(&buf, name, root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// addBuildArtifacts emits the inline Containerfile and any build context files.
// Every emitted path is registered in owners so two builds that resolve to the
// same artifact path are a hard error rather than a silent overwrite (the same
// guarantee BuildFileSet gives unit files). Type mismatches in the build data
// fail loudly instead of silently producing an empty file or a default mode:
// render validates its inputs rather than trusting the schema to have done so,
// since a silently-wrong file mode is a nasty failure to track down.
func addBuildArtifacts(files map[string]FileContent, owners map[string]string, owner string, u eval.UnitRecord) error {
	add := func(path string, fc FileContent) error {
		if err := ensureLocal(path); err != nil {
			return err
		}
		if prev, ok := owners[path]; ok {
			return fmt.Errorf("duplicate output file %q: emitted by both quadlet %q and quadlet %q", path, prev, owner)
		}
		owners[path] = owner
		files[path] = fc
		return nil
	}

	if v, present := u.Data["ContainerFile"]; present {
		cf, ok := v.(string)
		if !ok {
			return fmt.Errorf("value of ContainerFile must be a string, got %T", v)
		}
		if err := add("images/"+u.Stem+".Containerfile", FileContent{Content: []byte(cf)}); err != nil {
			return err
		}
	}

	v, present := u.Data["Context"]
	if !present {
		return nil
	}
	ctx, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("value of Context must be a map, got %T", v)
	}
	keys := make([]string, 0, len(ctx))
	for k := range ctx {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, p := range keys {
		content, mode, err := contextEntry(p, ctx[p])
		if err != nil {
			return err
		}
		if err := add("images/"+u.Stem+".context/"+p, FileContent{Content: []byte(content), Mode: mode}); err != nil {
			return err
		}
	}
	return nil
}

// contextEntry normalizes one build-context entry: a plain string (mode 0644)
// or a {content, mode} struct. A present-but-wrong-typed content or mode is an
// error, not a silent default, so malformed data never yields an empty file or
// a wrong (e.g. non-executable) mode. An omitted mode defaults to 0644.
func contextEntry(name string, v any) (content, mode string, err error) {
	switch x := v.(type) {
	case string:
		return x, "0644", nil
	case map[string]any:
		c, ok := x["content"].(string)
		if !ok {
			return "", "", fmt.Errorf("context entry %q: content must be a string, got %T", name, x["content"])
		}
		switch m := x["mode"].(type) {
		case nil:
			return c, "0644", nil // mode omitted
		case string:
			if m == "" {
				return c, "0644", nil
			}
			return c, m, nil
		default:
			return "", "", fmt.Errorf("context entry %q: mode must be a string, got %T", name, m)
		}
	default:
		return "", "", fmt.Errorf("context entry %q: unexpected type %T", name, v)
	}
}
