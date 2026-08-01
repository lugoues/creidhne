package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cuelang.org/go/cue/format"

	"github.com/lugoues/creidhne/internal/eval"
)

// loadSecretRegistry loads the project's crei-owned secret registry
// (registries/secrets.cue) with the schema overlay. Mirrors loadImages.
func loadSecretRegistry() ([]eval.SecretEntry, string, error) {
	cfg, err := resolveConfig()
	if err != nil {
		return nil, "", err
	}
	overlay, err := buildOverlay(cfg.ProjectDir)
	if err != nil {
		return nil, "", err
	}
	entries, err := eval.LoadSecretRegistry(cfg.ProjectDir, overlay)
	return entries, cfg.ProjectDir, err
}

// emitSecretRegistry regenerates registries/secrets.cue from the entries. crei
// owns this file, so it is rewritten canonically (format.Source, tabs) rather
// than surgically patched. The decoded fields (name, generate) are the whole
// managed schema today; extend both this and eval.LoadSecretRegistry together
// when a field is added. Secret material is never written here.
func emitSecretRegistry(entries []eval.SecretEntry) ([]byte, error) {
	// Sort a copy: callers hold indexes into their slice, and reordering it
	// underneath them would retarget those indexes (see emitImageRegistry).
	sorted := make([]eval.SecretEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

	var b bytes.Buffer
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	w("package registries")
	w("")
	w("import %q", eval.ModulePath)
	w("")
	w("// Managed by crei (crei secret create/rotate). Declares the podman secrets")
	w("// crei owns and how to generate them; secret values never live here.")
	w("secrets: creidhne.#SecretRegistry & {")
	for _, e := range sorted {
		var fields []string
		// name is emitted only when it differs from the key (it defaults to the
		// key in the schema), keeping the common case a one-liner.
		if e.Name != "" && e.Name != e.Key {
			fields = append(fields, fmt.Sprintf("name: %q", e.Name))
		}
		if g := e.Generate; g != nil {
			var gf []string
			if g.Length != 0 {
				gf = append(gf, fmt.Sprintf("length: %d", g.Length))
			}
			if g.Charset != "" && g.Charset != "alphanumeric" {
				gf = append(gf, fmt.Sprintf("charset: %q", g.Charset))
			}
			fields = append(fields, "generate: {"+strings.Join(gf, ", ")+"}")
		}
		switch {
		case len(fields) == 0:
			// No metadata: the entry is just a declared name. `_` marks it
			// present-but-unspecified, the same shape a hand-authored registry
			// uses.
			w("\t%s: _", cueKey(e.Key))
		case len(fields) == 1:
			w("\t%s: %s", cueKey(e.Key), fields[0])
		default:
			w("\t%s: {%s}", cueKey(e.Key), strings.Join(fields, ", "))
		}
	}
	w("}")
	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format registries/secrets.cue: %w", err)
	}
	return formatted, nil
}

// writeSecretRegistry regenerates registries/secrets.cue from entries. It
// creates the registries/ dir if absent so `secret create` works before an
// explicit `crei init` scaffolds it.
func writeSecretRegistry(projectDir string, entries []eval.SecretEntry) error {
	content, err := emitSecretRegistry(entries)
	if err != nil {
		return err
	}
	dir := filepath.Join(projectDir, "registries")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, "secrets.cue")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// findSecret returns the index of the named entry in the crei-owned registry,
// or an error listing what is available so a typo fails loudly.
func findSecret(entries []eval.SecretEntry, name string) (int, error) {
	var available []string
	for i := range entries {
		if entries[i].Key == name {
			return i, nil
		}
		available = append(available, entries[i].Key)
	}
	return -1, fmt.Errorf("no secret named %q in registries/secrets.cue (available: %s)", name, strings.Join(available, ", "))
}

// secretPolicies maps a podman secret name to its generate policy, for entries
// in the crei-owned registry that carry one. Used by create/rotate to
// synthesize a value non-interactively.
func secretPolicies(cfg config) (map[string]*eval.SecretGenerate, error) {
	overlay, err := buildOverlay(cfg.ProjectDir)
	if err != nil {
		return nil, err
	}
	reg, err := eval.LoadSecretRegistry(cfg.ProjectDir, overlay)
	if err != nil {
		return nil, err
	}
	m := make(map[string]*eval.SecretGenerate, len(reg))
	for _, e := range reg {
		if e.Generate != nil {
			m[e.Name] = e.Generate
		}
	}
	return m, nil
}

// declaredSecretNames is the union of every podman secret name the project
// declares: the hand-authored top-level registry (cfg.SecretsField) plus the
// crei-owned registries/secrets.cue. Deduplicated and sorted. This is what the
// list/create/prune commands reconcile against, so a secret registered either
// way is seen. A generate policy, when set, rides on the entries returned by
// loadSecretRegistry; here only names matter.
func declaredSecretNames(cfg config) ([]string, error) {
	overlay, err := buildOverlay(cfg.ProjectDir)
	if err != nil {
		return nil, err
	}
	names, err := eval.SecretRegistry(cfg.ProjectDir, overlay, cfg.SecretsField)
	if err != nil {
		return nil, err
	}
	reg, err := eval.LoadSecretRegistry(cfg.ProjectDir, overlay)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, e := range reg {
		if !seen[e.Name] {
			seen[e.Name] = true
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out, nil
}
