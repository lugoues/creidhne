// Package render turns evaluated unit data into the final Quadlet unit files by
// executing the embedded Go text/templates. Templating moved out of CUE into Go
// so the CUE->Go boundary is plain data; the .tpl files are unchanged.
package render

import (
	"bytes"
	"fmt"
	"io/fs"
	"sort"
	"text/template"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/kinds"
)

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
	tmpl, err := template.New("creidhne").Funcs(funcMap).ParseFS(tplFS, "*.tpl")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	for kind := range kinds.Ext {
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
				if err := addBuildArtifacts(files, u); err != nil {
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
// Context entries are either a plain string (mode 0644) or a {content, mode}
// struct; the normalization the prototype did in CUE happens here instead.
func addBuildArtifacts(files map[string]FileContent, u eval.UnitRecord) error {
	if cf, ok := u.Data["ContainerFile"].(string); ok {
		files["images/"+u.Stem+".Containerfile"] = FileContent{Content: []byte(cf)}
	}
	ctx, ok := u.Data["Context"].(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(ctx))
	for k := range ctx {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, p := range keys {
		var content, mode string
		switch x := ctx[p].(type) {
		case string:
			content, mode = x, "0644"
		case map[string]any:
			content, _ = x["content"].(string)
			if m, ok := x["mode"].(string); ok && m != "" {
				mode = m
			} else {
				mode = "0644"
			}
		default:
			return fmt.Errorf("context entry %q: unexpected type %T", p, ctx[p])
		}
		files["images/"+u.Stem+".context/"+p] = FileContent{Content: []byte(content), Mode: mode}
	}
	return nil
}
