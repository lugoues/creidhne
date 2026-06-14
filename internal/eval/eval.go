// Package eval loads CUE quadlet definitions and extracts the rendered-data
// manifest that the Go renderer consumes. It replaces the prototype's
// `cue export ./... --out json` + output.files merge with an in-process
// cuelang.org/go evaluation.
package eval

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
)

// ModulePath is the CUE module import path of the embedded schema.
const ModulePath = "github.com/lugoues/creidhne"

// UnitRecord is one entry of a #Quadlet's manifest: a unit's identity plus the
// concrete data the matching text/template renders.
type UnitRecord struct {
	Kind     string
	Stem     string
	Filename string
	Service  string
	Data     map[string]any
}

// Quadlet is a named collection of units (one top-level #Quadlet value).
type Quadlet struct {
	Name  string
	Units []UnitRecord
}

// LoadAndValidate loads the CUE package in dir and extracts every #Quadlet's
// manifest. If overlay is non-empty it is merged into the load config (used to
// vendor the embedded schema offline); pass nil to resolve dependencies from
// disk (e.g. the testing module's cue.mod/usr symlink).
func LoadAndValidate(dir string, overlay map[string]load.Source) ([]Quadlet, error) {
	cfg := &load.Config{Dir: dir}
	if len(overlay) > 0 {
		cfg.Overlay = overlay
	}
	insts := load.Instances([]string{"."}, cfg)
	if len(insts) == 0 {
		return nil, fmt.Errorf("no CUE instances found in %s", dir)
	}
	if err := insts[0].Err; err != nil {
		return nil, fmt.Errorf("load %s: %w", dir, err)
	}
	ctx := cuecontext.New()
	v := ctx.BuildInstance(insts[0])
	if err := v.Err(); err != nil {
		return nil, fmt.Errorf("build %s: %w", dir, err)
	}
	return extractQuadlets(v)
}

// extractQuadlets finds every top-level #Quadlet value (one with a manifest
// list). Test fixtures wrap their quadlet in `test.subject`, so that one level
// of nesting is also probed.
func extractQuadlets(v cue.Value) ([]Quadlet, error) {
	var out []Quadlet
	iter, err := v.Fields()
	if err != nil {
		return nil, err
	}
	for iter.Next() {
		fv := iter.Value()
		if q, ok, err := tryQuadlet(fv); err != nil {
			return nil, err
		} else if ok {
			out = append(out, q)
			continue
		}
		if sub := fv.LookupPath(cue.ParsePath("subject")); sub.Exists() {
			if q, ok, err := tryQuadlet(sub); err != nil {
				return nil, err
			} else if ok {
				out = append(out, q)
			}
		}
	}
	return out, nil
}

// tryQuadlet decodes v as a #Quadlet if it carries a manifest list.
func tryQuadlet(v cue.Value) (Quadlet, bool, error) {
	mf := v.LookupPath(cue.ParsePath("manifest"))
	if !mf.Exists() {
		return Quadlet{}, false, nil
	}
	list, err := mf.List()
	if err != nil {
		return Quadlet{}, false, nil // has a manifest field but it isn't a list
	}
	name, _ := v.LookupPath(cue.ParsePath("name")).String()
	q := Quadlet{Name: name}
	for list.Next() {
		rec := list.Value()
		ur := UnitRecord{}
		ur.Kind, _ = rec.LookupPath(cue.ParsePath("kind")).String()
		ur.Stem, _ = rec.LookupPath(cue.ParsePath("stem")).String()
		ur.Filename, _ = rec.LookupPath(cue.ParsePath("filename")).String()
		ur.Service, _ = rec.LookupPath(cue.ParsePath("service")).String()
		b, err := rec.LookupPath(cue.ParsePath("data")).MarshalJSON()
		if err != nil {
			return Quadlet{}, false, fmt.Errorf("marshal data for %s: %w", ur.Filename, err)
		}
		m, err := decodeJSONNumbers(b)
		if err != nil {
			return Quadlet{}, false, err
		}
		ur.Data = m
		q.Units = append(q.Units, ur)
	}
	return q, true, nil
}

// decodeJSONNumbers unmarshals JSON into map[string]any, converting integral
// numbers to int64 so templates' {{ printf "%d" }} render integers rather than
// %!d(float64=N).
func decodeJSONNumbers(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	m, _ := coerceNumbers(raw).(map[string]any)
	return m, nil
}

func coerceNumbers(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = coerceNumbers(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = coerceNumbers(e)
		}
		return x
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		if f, err := x.Float64(); err == nil {
			return f
		}
		return x.String()
	default:
		return v
	}
}

// Overlay vendors the embedded schema module under
// moduleRoot/cue.mod/usr/<ModulePath>/..., mirroring the repo's on-disk symlink
// layout so `import "github.com/lugoues/creidhne@v0"` resolves offline. schemaFS
// must contain the module under a top-level "creidhne/" directory.
func Overlay(moduleRoot string, schemaFS fs.FS) (map[string]load.Source, error) {
	overlay := map[string]load.Source{}
	err := fs.WalkDir(schemaFS, "creidhne", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(schemaFS, p)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, "creidhne/")
		abs := filepath.Join(moduleRoot, "cue.mod", "usr", filepath.FromSlash(ModulePath), filepath.FromSlash(rel))
		overlay[abs] = load.FromBytes(b)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return overlay, nil
}

// FindModuleRoot walks up from dir to the directory containing cue.mod.
func FindModuleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if fi, err := os.Stat(filepath.Join(abs, "cue.mod")); err == nil && fi.IsDir() {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no cue.mod found from %s upward", dir)
		}
		abs = parent
	}
}
