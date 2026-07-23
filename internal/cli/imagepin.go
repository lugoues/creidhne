package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"cuelang.org/go/cue/format"
	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/registry"
)

// cueIdent is a bare CUE identifier; other keys are quoted in emitted source.
var cueIdent = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func cueKey(k string) string {
	if cueIdent.MatchString(k) {
		return k
	}
	return strconv.Quote(k)
}

func newImagePinCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "pin [name...]",
		Short: "Resolve tracked tags to current digests and write them back",
		Long: "pin fills the gaps: entries with a tag but no digest get the tag's\n" +
			"current digest written back to registries/images.cue. Already-pinned\n" +
			"entries are left alone (crei image update is the verb that moves\n" +
			"existing pins, honoring min-age and semver-range policy); unmanaged\n" +
			"entries (digest, no tag) too. The change is a reviewable config edit.\n" +
			"Given names, only those entries are pinned.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, projectDir, err := loadImages()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(entries) == 0 {
				fmt.Fprintln(out, "No image registry (registries/images.cue).")
				return nil
			}
			only := map[string]bool{}
			for _, a := range args {
				only[a] = true
			}

			changed := 0
			for i := range entries {
				if len(only) > 0 && !only[entries[i].Key] {
					continue
				}
				r, err := registry.Parse(entries[i].Image)
				if err != nil {
					fmt.Fprintln(out, yellow("! "+entries[i].Key+": "+err.Error()))
					continue
				}
				if r.Tag == "" { // unmanaged: no channel to resolve
					fmt.Fprintf(out, "  %s %s (no tag, unchanged)\n", dim("-"), entries[i].Key)
					continue
				}
				// pin only fills gaps; moving an existing pin is update's job
				// (which respects min-age and semver-range policy).
				if entries[i].Digest != "" {
					fmt.Fprintf(out, "  %s %s (pinned; use 'crei image update' to advance)\n", dim("-"), entries[i].Key)
					continue
				}
				digest, err := registry.Digest(r.TaggedRef())
				if err != nil {
					fmt.Fprintln(out, yellow("! "+entries[i].Key+": "+firstLine(err.Error())))
					continue
				}

				fmt.Fprintf(out, "  %s %s -> %s\n", green("~"), entries[i].Key, short(digest))
				entries[i].Digest = digest // pin writes only the digest field
				changed++
			}
			if changed == 0 {
				fmt.Fprintln(out, "Nothing to pin.")
				return nil
			}

			path := filepath.Join(projectDir, "registries", "images.cue")
			content, err := emitImageRegistry(entries)
			if err != nil {
				return err
			}
			if !yes {
				ok, err := confirm(cmd.InOrStdin(), out, fmt.Sprintf("Write %d pin(s) to registries/images.cue?", changed))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
			}
			if err := os.WriteFile(path, content, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			fmt.Fprintf(out, "\nWrote %d pin(s). Run 'crei plan' to see the change.\n", changed)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

// emitImageRegistry regenerates registries/images.cue from the entries. crei
// owns this file, so it is rewritten canonically (cue fmt) rather than
// surgically patched; the entry fields decoded by LoadImageRegistry (ref,
// minAge) are the whole schema today, so nothing is lost. Extend both when a
// policy field is added.
func emitImageRegistry(entries []eval.ImageEntry) ([]byte, error) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	var b bytes.Buffer
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	w("package registries")
	w("")
	w("import %q", eval.ModulePath)
	w("")
	w("// Managed by crei (crei image pin). Each entry's image is the tracked")
	w("// channel (edit it here); crei image pin writes the digest.")
	w("images: creidhne.#ImageRegistry & {")
	for _, e := range entries {
		var fields []string
		fields = append(fields, fmt.Sprintf("image: %q", e.Image))
		if e.Digest != "" {
			fields = append(fields, fmt.Sprintf("digest: %q", e.Digest))
		}
		if e.MinAge != "" {
			fields = append(fields, fmt.Sprintf("minAge: %q", e.MinAge))
		}
		if e.Range != "" {
			fields = append(fields, fmt.Sprintf("range: %q", e.Range))
		}
		if len(fields) == 1 {
			w("\t%s: %s", cueKey(e.Key), fields[0])
		} else {
			w("\t%s: {%s}", cueKey(e.Key), strings.Join(fields, ", "))
		}
	}
	w("}")
	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format registries/images.cue: %w", err)
	}
	return formatted, nil
}
