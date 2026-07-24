package eval

import (
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
)

// SecretEntry is one crei-owned secret registry entry decoded from
// registries/secrets.cue, for the crei secret commands. It carries only
// management metadata: the podman name and the optional generation policy.
// Secret material never lives in CUE.
type SecretEntry struct {
	Key      string
	Name     string // podman secret name; defaults to Key
	Generate *SecretGenerate
}

// SecretGenerate is a secret's value-synthesis policy: how create/rotate make
// a value. Nil means the value is supplied by hand, not generated.
type SecretGenerate struct {
	Length  int
	Charset string // "alphanumeric" (default), "hex", or "base64"
}

// LoadSecretRegistry loads dir/registries and decodes its `secrets` map. A
// missing registries package (or no secrets field there) returns (nil, nil):
// the crei-owned secret registry is optional, exactly like the image one. A
// present-but-broken package is a real error.
func LoadSecretRegistry(dir string, overlay map[string]load.Source) ([]SecretEntry, error) {
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
	secrets := v.LookupPath(cue.ParsePath("secrets"))
	if !secrets.Exists() {
		return nil, nil
	}
	it, err := secrets.Fields()
	if err != nil {
		return nil, fmt.Errorf("read secrets registry: %w", err)
	}
	var out []SecretEntry
	for it.Next() {
		key := it.Selector().Unquoted()
		e := SecretEntry{Key: key, Name: key}
		val := it.Value()
		if f := val.LookupPath(cue.ParsePath("name")); f.Exists() {
			if s, err := f.String(); err == nil && s != "" {
				e.Name = s
			}
		}
		if g := val.LookupPath(cue.ParsePath("generate")); g.Exists() {
			gen := &SecretGenerate{}
			if l := g.LookupPath(cue.ParsePath("length")); l.Exists() {
				if n, err := l.Int64(); err == nil {
					gen.Length = int(n)
				}
			}
			if c := g.LookupPath(cue.ParsePath("charset")); c.Exists() {
				gen.Charset, _ = c.String()
			}
			e.Generate = gen
		}
		out = append(out, e)
	}
	return out, nil
}
