package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

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
		Long: "pin resolves each managed entry's tag to its current registry digest\n" +
			"and rewrites registries/images.cue with the pinned refs. Unpinned\n" +
			"entries (tag, no digest) become pinned; managed entries refresh to the\n" +
			"tag's current digest; unmanaged entries (digest, no tag) are left alone.\n" +
			"The change is a reviewable config edit — apply follows normally. Given\n" +
			"names, only those entries are pinned.",
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
				r, err := registry.Parse(entries[i].Ref)
				if err != nil {
					fmt.Fprintln(out, yellow("! "+entries[i].Key+": "+err.Error()))
					continue
				}
				if r.Tag == "" { // unmanaged: nothing to resolve
					fmt.Fprintf(out, "  %s %s (no tag, unchanged)\n", dim("-"), entries[i].Key)
					continue
				}
				digest, err := registry.Digest(r.TaggedRef())
				if err != nil {
					fmt.Fprintln(out, yellow("! "+entries[i].Key+": "+firstLine(err.Error())))
					continue
				}
				pinned := r.Pinned(digest)
				if pinned == entries[i].Ref {
					fmt.Fprintf(out, "  %s %s (up to date)\n", dim("-"), entries[i].Key)
					continue
				}
				fmt.Fprintf(out, "  %s %s -> %s\n", green("~"), entries[i].Key, short(digest))
				entries[i].Ref = pinned
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
	w("// Managed by crei (crei image pin). Entries track a tag and pin a digest;")
	w("// edit refs/policy here, then 'crei image pin' refreshes the digests.")
	w("images: creidhne.#ImageRegistry & {")
	for _, e := range entries {
		if e.MinAge != "" {
			w("\t%s: {ref: %q, minAge: %q}", cueKey(e.Key), e.Ref, e.MinAge)
		} else {
			w("\t%s: ref: %q", cueKey(e.Key), e.Ref)
		}
	}
	w("}")
	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format registries/images.cue: %w", err)
	}
	return formatted, nil
}
