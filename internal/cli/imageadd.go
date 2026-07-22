package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/registry"
)

func newImageAddCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "add [name] <ref>",
		Short: "Add an image to the registry and pin it in one step",
		Long: "add appends an entry to registries/images.cue and resolves its digest.\n" +
			"With one argument the entry name is derived from the image (the last\n" +
			"repository segment, sanitized to a CUE identifier); pass an explicit name\n" +
			"first to override. If the ref already carries a digest it is used as-is;\n" +
			"a tag with no digest is resolved now; a bare ref tracks :latest (podman\n" +
			"semantics) and pins its current digest.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name, ref string
			if len(args) == 2 {
				name, ref = args[0], args[1]
			} else {
				ref = args[0]
			}
			r, err := registry.Parse(ref)
			if err != nil {
				return err
			}
			if name == "" {
				name = deriveName(r.Repo)
			}
			if name == "" {
				return fmt.Errorf("could not derive a name from %q; pass one explicitly", ref)
			}

			entries, projectDir, err := loadImages()
			if err != nil {
				return err
			}
			for _, e := range entries {
				if e.Key == name && !force {
					return fmt.Errorf("%q already in the registry; use 'crei image pin %s' to refresh, or --force to replace", name, name)
				}
			}

			out := cmd.OutOrStdout()
			// A bare ref means :latest (podman semantics); make the channel
			// explicit so the entry is trackable and pins now.
			if r.Tag == "" && r.Digest == "" {
				r.Tag = "latest"
				fmt.Fprintln(out, dim("  no tag given — tracking :latest"))
			}
			image := r.Repo
			if r.Tag != "" {
				image = r.Repo + ":" + r.Tag
			}
			digest := r.Digest // used as-is when pasted
			if digest == "" {
				digest, err = registry.Digest(r.TaggedRef())
				if err != nil {
					return fmt.Errorf("resolve %s: %w", r.TaggedRef(), err)
				}
			}

			// Replace an existing entry (--force) or append.
			entries = upsertImage(entries, eval.ImageEntry{Key: name, Image: image, Digest: digest})
			content, err := emitImageRegistry(entries)
			if err != nil {
				return err
			}
			path := filepath.Join(projectDir, "registries", "images.cue")
			if err := os.WriteFile(path, content, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			pin := "unpinned"
			if digest != "" {
				pin = short(digest)
			}
			fmt.Fprintf(out, "%s %s = %s (%s)\n", green("+"), name, image, pin)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing entry of the same name")
	return cmd
}

// deriveName turns a repository into a CUE-identifier entry key: the last path
// segment, with non-identifier characters folded to "_" and a leading digit
// prefixed.
func deriveName(repo string) string {
	seg := repo
	if i := strings.LastIndex(seg, "/"); i >= 0 {
		seg = seg[i+1:]
	}
	var b strings.Builder
	for i, r := range seg {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// upsertImage replaces an entry with the same key or appends a new one.
func upsertImage(entries []eval.ImageEntry, e eval.ImageEntry) []eval.ImageEntry {
	for i := range entries {
		if entries[i].Key == e.Key {
			entries[i] = e
			return entries
		}
	}
	return append(entries, e)
}
