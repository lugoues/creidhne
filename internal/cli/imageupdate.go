package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/registry"
)

// candidate is an entry's policy-approved next state, computed by nextPin.
type candidate struct {
	Tag    string // the tag to track (advanced within range when one is set)
	Digest string // that tag's current digest
	Held   string // non-empty: human reason the candidate is withheld (min-age)
	Reason string // what changed: "digest", "tag 8.25.3 -> 8.26.0", "" if current
}

// nextPin resolves a managed entry's next pin: advance the tag within the
// semver range (when set), resolve its digest, and apply min-age policy.
// The caller guarantees the entry has a tag.
func nextPin(e eval.ImageEntry, r registry.Ref, defAge time.Duration, now time.Time, res resolver) (candidate, error) {
	c := candidate{Tag: r.Tag}
	if e.Range != "" {
		tags, err := res.tags(r.Repo)
		if err != nil {
			return c, fmt.Errorf("%s: %w", e.Key, err)
		}
		pick, err := registry.PickVersion(tags, e.Range)
		if err != nil {
			return c, fmt.Errorf("%s: %w", e.Key, err)
		}
		if pick != "" {
			c.Tag = pick
		}
	}
	digest, err := res.digest(r.Repo + ":" + c.Tag)
	if err != nil {
		return c, fmt.Errorf("%s: resolve %s:%s: %w", e.Key, r.Repo, c.Tag, err)
	}
	c.Digest = digest
	if c.Tag == r.Tag && digest == e.Digest {
		return c, nil // current
	}
	switch {
	case c.Tag != r.Tag:
		c.Reason = fmt.Sprintf("tag %s -> %s", r.Tag, c.Tag)
	default:
		c.Reason = "digest"
	}

	effAge := defAge
	if e.MinAge != "" {
		effAge, _ = registry.ParseAge(e.MinAge) // schema regex already validated
	}
	if effAge > 0 {
		if created, err := res.created(r.Repo + "@" + digest); err == nil {
			if age := now.Sub(created); age < effAge {
				c.Held = fmt.Sprintf("held: candidate is %s old (min-age %s)", humanDuration(age), humanDuration(effAge))
			}
		}
	}
	return c, nil
}

func newImageUpdateCmd() *cobra.Command {
	var yes bool
	var minAgeFlag string
	cmd := &cobra.Command{
		Use:   "update [name...]",
		Short: "Advance pinned digests (and ranged tags) through the config",
		Long: "update moves managed entries forward: each tracked tag is resolved to\n" +
			"its current digest, entries with a semver range first advance the tag\n" +
			"itself to the highest in-range version, and candidates younger than the\n" +
			"min-age policy are held. Changes are written back to\n" +
			"registries/images.cue — a reviewable config edit; apply follows\n" +
			"normally. Given names, only those entries are updated. (pin only fills\n" +
			"missing digests; update is the verb that moves existing pins.)",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			defAge, err := registry.ParseAge(minAgeFlag)
			if err != nil {
				return err
			}
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
			res := liveResolver()
			changed := 0
			for i := range entries {
				e := entries[i]
				if len(only) > 0 && !only[e.Key] {
					continue
				}
				r, err := registry.Parse(e.Image)
				if err != nil {
					fmt.Fprintln(out, yellow("! "+e.Key+": "+err.Error()))
					continue
				}
				if r.Tag == "" {
					fmt.Fprintf(out, "  %s %s (no tag, unchanged)\n", dim("-"), e.Key)
					continue
				}
				c, err := nextPin(e, r, defAge, time.Now(), res)
				if err != nil {
					fmt.Fprintln(out, yellow("! "+firstLine(err.Error())))
					continue
				}
				switch {
				case c.Reason == "":
					fmt.Fprintf(out, "  %s %s (up to date)\n", dim("-"), e.Key)
				case c.Held != "":
					fmt.Fprintf(out, "  %s %s (%s)\n", dim("-"), e.Key, c.Held)
				default:
					fmt.Fprintf(out, "  %s %s: %s -> %s\n", green("~"), e.Key, c.Reason, short(c.Digest))
					entries[i].Image = r.Repo + ":" + c.Tag
					entries[i].Digest = c.Digest
					changed++
				}
			}
			if changed == 0 {
				fmt.Fprintln(out, "Nothing to update.")
				return nil
			}
			content, err := emitImageRegistry(entries)
			if err != nil {
				return err
			}
			if !yes {
				ok, err := confirm(cmd.InOrStdin(), out, fmt.Sprintf("Write %d update(s) to registries/images.cue?", changed))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
			}
			path := filepath.Join(projectDir, "registries", "images.cue")
			if err := os.WriteFile(path, content, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			fmt.Fprintf(out, "\nWrote %d update(s). Run 'crei plan' to see the change.\n", changed)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.Flags().StringVar(&minAgeFlag, "min-age", "", "hold candidates younger than this (e.g. 7d); per-entry minAge overrides")
	return cmd
}
