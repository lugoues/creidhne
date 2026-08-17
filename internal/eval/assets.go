package eval

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// expandAssetContexts resolves {asset: "<glob>"} build-context entries into
// ordinary inline {content, mode} entries, reading the matched project files.
// It runs in LoadAndValidate before injectBuildHashes, so the file bytes are
// part of the build's content hash: editing an asset moves the .build file and
// flags every consuming container stale, exactly like an inline-context edit.
// Everything downstream (render, state, golden) sees plain context entries and
// needs no asset awareness.
//
// Mapping: the glob's static prefix is the root; each matched file lands at
// <Context key>/<path relative to that prefix> ("." keys the context root).
// Matches are sorted, so output and hashes are deterministic. A glob that
// matches nothing is an error: a typo silently shipping an empty context is
// the failure mode this feature exists to prevent.
func expandAssetContexts(dir string, quads []Quadlet) error {
	for _, q := range quads {
		for _, u := range q.Units {
			if u.Kind != "build" {
				continue
			}
			ctx, ok := u.Data["Context"].(map[string]any)
			if !ok {
				continue
			}
			// Collect the asset keys first: expansion mutates the map.
			var assetKeys []string
			for key, v := range ctx {
				if m, ok := v.(map[string]any); ok {
					if _, isAsset := m["asset"]; isAsset {
						assetKeys = append(assetKeys, key)
					}
				}
			}
			sort.Strings(assetKeys)
			for _, key := range assetKeys {
				glob, _ := ctx[key].(map[string]any)["asset"].(string)
				files, err := resolveAssetGlob(dir, glob)
				if err != nil {
					return fmt.Errorf("%s: Context %q: %w", u.Filename, key, err)
				}
				delete(ctx, key)
				for _, f := range files {
					dest := path.Join(key, f.rel) // Join cleans; "." keys the root
					if _, exists := ctx[dest]; exists {
						return fmt.Errorf("%s: Context %q: asset %q collides with existing context entry %q", u.Filename, key, f.rel, dest)
					}
					ctx[dest] = map[string]any{"content": f.content, "mode": f.mode}
				}
			}
		}
	}
	return nil
}

// assetFile is one resolved asset: its path relative to the glob's static
// prefix, its content, and its mode (0755 when executable on disk, else 0644).
type assetFile struct {
	rel     string
	content string
	mode    string
}

// resolveAssetGlob matches a project-relative doublestar glob and reads the
// matched regular files. The glob must stay inside the project: absolute paths
// and ".." segments are rejected up front.
func resolveAssetGlob(dir, glob string) ([]assetFile, error) {
	if filepath.IsAbs(glob) || strings.HasPrefix(glob, "/") {
		return nil, fmt.Errorf("asset glob %q must be project-relative", glob)
	}
	for _, seg := range strings.Split(glob, "/") {
		if seg == ".." {
			return nil, fmt.Errorf("asset glob %q must not escape the project (..)", glob)
		}
	}
	if !doublestar.ValidatePattern(glob) {
		return nil, fmt.Errorf("invalid asset glob %q", glob)
	}

	fsys := os.DirFS(dir)
	matches, err := doublestar.Glob(fsys, glob, doublestar.WithFilesOnly())
	if err != nil {
		return nil, fmt.Errorf("asset glob %q: %w", glob, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("asset glob %q matches no files", glob)
	}
	sort.Strings(matches)

	// The static prefix (everything before the first meta character) is the
	// root the destination paths are made relative to.
	base, _ := doublestar.SplitPattern(glob)

	// The lexical ".." check above cannot see symlinks, and both the glob and
	// the reads below follow them. Resolve every match and require it to stay
	// beneath the resolved project root, or a symlink inside the project would
	// smuggle outside files into the build context.
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve project dir: %w", err)
	}

	out := make([]assetFile, 0, len(matches))
	for _, m := range matches {
		full := filepath.Join(dir, filepath.FromSlash(m))
		resolved, err := filepath.EvalSymlinks(full)
		if err != nil {
			return nil, fmt.Errorf("asset %q: %w", m, err)
		}
		if rel, relErr := filepath.Rel(root, resolved); relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("asset %q resolves to %s, outside the project", m, resolved)
		}
		// Stat and read the resolved path, so the bytes shipped are the bytes
		// the containment check validated.
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("asset %q: %w", m, err)
		}
		// WithFilesOnly only excludes directories; a FIFO would make the
		// ReadFile below block forever, hanging every command that loads
		// the project.
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("asset %q: not a regular file (%s)", m, info.Mode())
		}
		content, err := os.ReadFile(resolved)
		if err != nil {
			return nil, fmt.Errorf("asset %q: %w", m, err)
		}
		rel := m
		if base != "." {
			rel = strings.TrimPrefix(strings.TrimPrefix(m, base), "/")
		}
		mode := "0644"
		if info.Mode()&0o111 != 0 {
			mode = "0755"
		}
		out = append(out, assetFile{rel: rel, content: string(content), mode: mode})
	}
	return out, nil
}
