package eval

import (
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
)

// ImageEntry is one #ImageRegistry entry decoded from the project's
// registries package, for the crei image commands.
type ImageEntry struct {
	Key    string
	Image  string // tracked ref, no digest
	Digest string // "sha256:…", "" when unpinned
	MinAge string // "" when unset
	Range  string // semver constraint for tag advancement, "" when unset
}

// LoadImageRegistry loads dir/registries and decodes its `images` map. A
// missing registries package (or no images field) returns (nil, nil): the
// registry is optional. A present-but-broken package is a real error.
func LoadImageRegistry(dir string, overlay map[string]load.Source) ([]ImageEntry, error) {
	if _, err := os.Stat(filepath.Join(dir, "registries")); os.IsNotExist(err) {
		return nil, nil
	}
	cfg := &load.Config{Dir: dir}
	if len(overlay) > 0 {
		cfg.Overlay = overlay
	}
	insts := load.Instances([]string{"./registries"}, cfg)
	if len(insts) == 0 {
		return nil, nil
	}
	if err := insts[0].Err; err != nil {
		return nil, cueError("load registries", err)
	}
	v := cuecontext.New().BuildInstance(insts[0])
	if err := v.Err(); err != nil {
		return nil, cueError("build registries", err)
	}
	images := v.LookupPath(cue.ParsePath("images"))
	if !images.Exists() {
		return nil, nil
	}
	it, err := images.Fields()
	if err != nil {
		return nil, fmt.Errorf("read images registry: %w", err)
	}
	var out []ImageEntry
	for it.Next() {
		e := ImageEntry{Key: it.Selector().Unquoted()}
		v := it.Value()
		if f := v.LookupPath(cue.ParsePath("image")); f.Exists() {
			e.Image, _ = f.String()
		}
		if f := v.LookupPath(cue.ParsePath("digest")); f.Exists() {
			e.Digest, _ = f.String()
		}
		if f := v.LookupPath(cue.ParsePath("minAge")); f.Exists() {
			e.MinAge, _ = f.String()
		}
		if f := v.LookupPath(cue.ParsePath("range")); f.Exists() {
			e.Range, _ = f.String()
		}
		out = append(out, e)
	}
	return out, nil
}
