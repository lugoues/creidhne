// Package eval loads CUE quadlet definitions and extracts, via an in-process
// cuelang.org/go evaluation, the rendered-data manifest that the Go renderer
// consumes.
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
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
	v, err := buildInstance(dir, overlay)
	if err != nil {
		return nil, err
	}
	return extractQuadlets(v)
}

// Validate performs the strict whole-package check that `crei validate` reports
// on: every regular value must be concrete and constraint-valid, equivalent to
// the prototype's `cue export ./...`. LoadAndValidate (used by render/apply)
// only forces the rendered unit data to be concrete, so Validate additionally
// catches incomplete values that never reach a rendered unit, e.g. an unset
// required field in a helper struct or an externals/secrets registry entry.
func Validate(dir string, overlay map[string]load.Source) error {
	v, err := buildInstance(dir, overlay)
	if err != nil {
		return err
	}
	return cueError("validate "+dir, v.Validate(cue.Concrete(true)))
}

// buildInstance loads and builds the CUE package in dir into a single value,
// surfacing load and structural (bottom) errors.
func buildInstance(dir string, overlay map[string]load.Source) (cue.Value, error) {
	cfg := &load.Config{Dir: dir}
	if len(overlay) > 0 {
		cfg.Overlay = overlay
	}
	insts := load.Instances([]string{"."}, cfg)
	if len(insts) == 0 {
		return cue.Value{}, fmt.Errorf("no CUE instances found in %s", dir)
	}
	if err := insts[0].Err; err != nil {
		return cue.Value{}, cueError("load "+dir, err)
	}
	v := cuecontext.New().BuildInstance(insts[0])
	if err := v.Err(); err != nil {
		return cue.Value{}, cueError("build "+dir, err)
	}
	return v, nil
}

// cueError expands a cuelang error so every underlying error is shown with its
// file position, instead of cue's default "<first error> (and N more errors)"
// truncation that hides all but one. A nil err returns nil.
func cueError(context string, err error) error {
	if err == nil {
		return nil
	}
	details := strings.TrimSpace(errors.Details(err, nil))
	if details == "" {
		details = err.Error()
	}
	return fmt.Errorf("%s:\n%s", context, details)
}

// SecretRegistry returns the podman secret names declared in the project's
// #SecretRegistry, read from the top-level field named `field` (e.g. "secrets").
// A missing field yields no names (no registry). Each entry's `name` (which
// defaults to its key) is the podman secret name; names are deduplicated and
// sorted. Only the registry needs to be concrete, so this works even when other
// parts of the project are incomplete.
func SecretRegistry(dir string, overlay map[string]load.Source, field string) ([]string, error) {
	v, err := buildInstance(dir, overlay)
	if err != nil {
		return nil, err
	}
	reg := v.LookupPath(cue.ParsePath(field))
	if !reg.Exists() {
		return nil, nil
	}
	iter, err := reg.Fields()
	if err != nil {
		return nil, fmt.Errorf("%q is not a secret registry (want a struct of {name: string} entries): %w", field, err)
	}
	seen := map[string]bool{}
	var names []string
	for iter.Next() {
		name, err := iter.Value().LookupPath(cue.ParsePath("name")).String()
		if err != nil {
			return nil, fmt.Errorf("%s.%s has no concrete string \"name\"; is %q your #SecretRegistry?", field, iter.Selector(), field)
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// extractQuadlets finds every #Quadlet value (one carrying a manifest list)
// reachable from v, descending through plain struct fields so quadlets nested
// under a grouping struct (e.g. `stacks: web: #Quadlet`) or wrapped by the test
// harness (`test: subject: #Quadlet`) are all discovered, not just top-level
// ones. Descent stops at each quadlet (its internal units are not re-scanned).
// Hidden (`_`-prefixed) fields are skipped, matching `cue export` semantics,
// they are treated as private (and are often incomplete base templates that
// would not render).
func extractQuadlets(v cue.Value) ([]Quadlet, error) {
	var out []Quadlet
	var visit func(cue.Value, int) error
	visit = func(val cue.Value, depth int) error {
		if depth > 100 {
			return nil // guard against pathological nesting
		}
		if q, ok, err := tryQuadlet(val); err != nil {
			return err
		} else if ok {
			out = append(out, q)
			return nil
		}
		if val.IncompleteKind() != cue.StructKind {
			return nil
		}
		iter, err := val.Fields()
		if err != nil {
			return nil // not iterable as a struct; nothing to descend into
		}
		for iter.Next() {
			if err := visit(iter.Value(), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(v, 0); err != nil {
		return nil, err
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
		kind, _ := rec.LookupPath(cue.ParsePath("kind")).String()
		if kind == "" {
			// A list-typed field named `manifest` whose records lack the `kind`
			// contract field is not a #Quadlet manifest (e.g. a user value that
			// happens to be named `manifest`). Skip it rather than failing with a
			// cryptic error. This matters now that discovery recurses the whole
			// value tree, not just top-level values.
			return Quadlet{}, false, nil
		}
		ur := UnitRecord{Kind: kind}
		ur.Stem, _ = rec.LookupPath(cue.ParsePath("stem")).String()
		ur.Filename, _ = rec.LookupPath(cue.ParsePath("filename")).String()
		ur.Service, _ = rec.LookupPath(cue.ParsePath("service")).String()
		dataV := rec.LookupPath(cue.ParsePath("data"))
		// Validate concreteness first: MarshalJSON on an incomplete unit emits a
		// multi-KB dump of the whole resolved struct; report a concise hint
		// instead and point at `crei validate` for the full diagnostic.
		if err := dataV.Validate(cue.Concrete(true)); err != nil {
			// The filename is derived from name, so when name itself is the unset
			// field the filename is empty too; fall back to the stem, then the
			// kind, so the message never reads "unit  is incomplete".
			label := ur.Filename
			if label == "" {
				label = ur.Stem
			}
			if label == "" {
				label = "a " + ur.Kind
			}
			return Quadlet{}, false, fmt.Errorf("unit %s is incomplete: %s", label, incompleteHint(err))
		}
		b, err := dataV.MarshalJSON()
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

// incompleteHint renders a concise message for an incomplete-value error. CUE
// dumps the entire resolved struct (kilobytes, especially for an unsatisfied
// disjunction like Container's Image|Rootfs), so trim it at the struct dump and
// point the user at `crei validate` for the full detail.
func incompleteHint(err error) string {
	msg := strings.TrimSpace(errors.Details(err, nil))
	if i := strings.IndexByte(msg, '{'); i > 0 {
		return strings.TrimSpace(msg[:i]) + " (a required field is unset; run 'crei validate' for details)"
	}
	const max = 200
	if len(msg) > max {
		return strings.TrimSpace(msg[:max]) + " …"
	}
	return msg
}

// decodeJSONNumbers unmarshals JSON into map[string]any, converting integral
// numbers to int64 so templates' {{ printf "%d" }} render integers rather than
// %!d(float64=N).
func decodeJSONNumbers(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	coerced, err := coerceNumbers(raw)
	if err != nil {
		return nil, err
	}
	m, ok := coerced.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("decoded unit data is %T, want a JSON object", coerced)
	}
	return m, nil
}

func coerceNumbers(v any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			c, err := coerceNumbers(e)
			if err != nil {
				return nil, err
			}
			x[k] = c
		}
		return x, nil
	case []any:
		for i, e := range x {
			c, err := coerceNumbers(e)
			if err != nil {
				return nil, err
			}
			x[i] = c
		}
		return x, nil
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i, nil
		}
		// Not int64-representable. An integer literal that doesn't fit is an
		// overflow: erroring beats the old float64 fallback, which rendered as
		// %!d(float64=N) in a {{ printf "%d" }} field — a silently corrupt unit.
		// A genuinely fractional literal still falls through to float64.
		if !strings.ContainsAny(x.String(), ".eE") {
			return nil, fmt.Errorf("integer %s is out of range (exceeds int64)", x.String())
		}
		if f, err := x.Float64(); err == nil {
			return f, nil
		}
		return x.String(), nil
	default:
		return v, nil
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
