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
)

// extByKind maps a unit kind to its Quadlet file extension. It also serves as
// the set of templates to load (one <kind>.tpl per key).
var extByKind = map[string]string{
	"container": ".container",
	"pod":       ".pod",
	"volume":    ".volume",
	"network":   ".network",
	"kube":      ".kube",
	"build":     ".build",
	"image":     ".image",
	"artifact":  ".artifact",
}

// FileContent is a rendered file plus an optional octal mode (set for build
// context files like executable scripts; empty means default permissions).
type FileContent struct {
	Content []byte
	Mode    string
}

// Renderer holds the parsed templates, one per unit kind.
type Renderer struct {
	tpl map[string]*template.Template
}

// New parses every <kind>.tpl from tplFS (expected to contain the templates at
// its root, e.g. fs.Sub(embeddedFS, "templates") or os.DirFS("templates")).
func New(tplFS fs.FS) (*Renderer, error) {
	r := &Renderer{tpl: make(map[string]*template.Template, len(extByKind))}
	for kind := range extByKind {
		b, err := fs.ReadFile(tplFS, kind+".tpl")
		if err != nil {
			return nil, fmt.Errorf("read template %q: %w", kind, err)
		}
		t, err := template.New(kind).Parse(string(b))
		if err != nil {
			return nil, fmt.Errorf("parse template %q: %w", kind, err)
		}
		r.tpl[kind] = t
	}
	return r, nil
}

// BuildFileSet renders every unit across all quadlets into a filename->content
// map, including build artifacts (images/<stem>.Containerfile and
// images/<stem>.context/<path>). This is the Go equivalent of the prototype's
// output.files.
func (r *Renderer) BuildFileSet(quadlets []eval.Quadlet) (map[string]FileContent, error) {
	files := make(map[string]FileContent)
	for _, q := range quadlets {
		for _, u := range q.Units {
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
	t, ok := r.tpl[u.Kind]
	if !ok {
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
	if err := t.Execute(&buf, root); err != nil {
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
