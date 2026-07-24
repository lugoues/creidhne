package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/registry"
)

// withSpinner runs a long fetch under huh's spinner when out is a terminal,
// plainly otherwise (piped/CI output stays clean).
func withSpinner(out io.Writer, title string, action func()) {
	if f, ok := out.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		if err := spinner.New().Title(title).Output(f).Action(action).Run(); err == nil {
			return
		}
		// Spinner failure must never eat the work: tea errors leave the
		// action unrun only before it started; fall through defensively.
	}
	action()
}

// candidate is an entry's available upgrade, computed by nextPin. There is no
// hold machinery: min-age is information (Age + Young), and the picker's
// selection is the judgment.
type candidate struct {
	Tag    string        // the tag to track (advanced for version-shaped tags)
	Digest string        // that tag's current digest
	Reason string        // "digest moved" or "v1 -> v2"; "" when current
	Age    time.Duration // time since the candidate image was created
	HasAge bool
	Young  bool // younger than the effective min-age (marker, not a gate)
}

// nextPin resolves a managed entry's upgrade candidate. Version-shaped tags
// advance within the implicit ">= current" (narrowed by an explicit range;
// "=x.y.z" freezes); float tags follow their own digest. The caller
// guarantees the entry has a tag.
func nextPin(e eval.ImageEntry, r registry.Ref, minAge time.Duration, now time.Time, res resolver) (candidate, error) {
	c := candidate{Tag: r.Tag}
	if cur, ok := registry.ParseVersion(r.Tag); ok {
		tags, err := res.tags(r.Repo)
		if err != nil {
			return c, fmt.Errorf("%s: %w", e.Key, err)
		}
		pick, err := registry.PickVersion(tags, cur, e.Range)
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
	if c.Tag != r.Tag {
		c.Reason = fmt.Sprintf("%s -> %s", r.Tag, c.Tag)
	} else {
		c.Reason = "digest moved"
	}

	if e.MinAge != "" {
		minAge, _ = registry.ParseAge(e.MinAge) // schema regex already validated
	}
	if created, err := res.created(r.Repo + "@" + digest); err == nil && !created.IsZero() {
		c.Age = now.Sub(created)
		c.HasAge = true
		c.Young = minAge > 0 && c.Age < minAge
	}
	return c, nil
}

// updateItem pairs an entry index with its candidate for the picker.
type updateItem struct {
	idx int
	e   eval.ImageEntry
	c   candidate
}

// label is multiline: the entry name (bold) carries the checkbox line,
// details (change, release age, digest) flow indented beneath it — long
// image names stop crowding a columnar layout.
func (it updateItem) label() string {
	name := bold(it.e.Key)
	if it.c.Young {
		name += "  ! younger than min-age"
	}
	age := "age unknown"
	if it.c.HasAge {
		age = "released " + humanDuration(it.c.Age) + " ago"
	}
	return fmt.Sprintf("%s\n  %s\n  %s\n  %s", name, it.c.Reason, age, short(it.c.Digest))
}

// splitCandidates resolves every in-scope entry and sorts the ones with a
// waiting update into selectable (items) and lock-held (held), plus per-entry
// lookup warnings. only, when non-empty, restricts the scope to those names.
//
// A lock diverts an entry to held even when it was named explicitly: naming
// overrides a min-age marker (youth is information) but never a lock (a lock is
// a decision, made when you had the context you now lack). This split is the
// whole behavioral contract, so it lives apart from the command for testing.
func splitCandidates(entries []eval.ImageEntry, only map[string]bool, defAge time.Duration, now time.Time, res resolver) (items, held []updateItem, warns []string) {
	for i := range entries {
		e := entries[i]
		if len(only) > 0 && !only[e.Key] {
			continue
		}
		r, err := registry.Parse(e.Image)
		if err != nil || r.Tag == "" {
			continue // invalid/unmanaged surface in outdated, not here
		}
		c, err := nextPin(e, r, defAge, now, res)
		if err != nil {
			warns = append(warns, firstLine(err.Error()))
			continue
		}
		if c.Reason == "" {
			continue // already current
		}
		if e.Lock != nil {
			held = append(held, updateItem{idx: i, e: e, c: c})
			continue
		}
		items = append(items, updateItem{idx: i, e: e, c: c})
	}
	return items, held, warns
}

// printHeld reports the candidates a lock is holding back, above the picker.
// They are shown rather than silently dropped: seeing the update exists, with
// the reason it is being refused, is the whole reason the lock records one.
func printHeld(out io.Writer, held []updateItem, at time.Time) {
	if len(held) == 0 {
		return
	}
	fmt.Fprintln(out, bold("Held by a lock (not offered):"))
	for _, it := range held {
		fmt.Fprintf(out, "  %s %s  %s\n", yellow("x"), bold(it.e.Key), dim(it.c.Reason))
		fmt.Fprintf(out, "      %s\n", lockNote(it.e.Lock, at))
	}
	fmt.Fprintf(out, "  %s\n\n", dim("run 'crei image unlock <name>' to release"))
}

func newImageUpdateCmd() *cobra.Command {
	var yes bool
	var minAgeFlag string
	cmd := &cobra.Command{
		Use:   "update [name...]",
		Short: "Pick and apply image updates (tags and digests) through the config",
		Long: "update finds what moved — version-shaped tags advance within their\n" +
			"implicit '>= current' (an explicit range narrows it; '=x.y.z' freezes),\n" +
			"floating tags follow their digest — and presents the candidates for\n" +
			"selection. Everything starts unselected: applying is an explicit\n" +
			"choice. Candidates younger than min-age (per-entry minAge, else\n" +
			"--min-age) carry a ! marker. The selection is written back to\n" +
			"registries/images.cue — a reviewable config edit; apply follows\n" +
			"normally.\n\n" +
			"-y skips the picker and applies every aged candidate; naming entries\n" +
			"restricts (and pre-selects) just those.\n\n" +
			"Locked entries (crei image lock) are listed with their reason but are\n" +
			"never offered, and naming one does not override that: unlock it first.",
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
			now := time.Now()
			var items, held []updateItem
			var warns []string
			withSpinner(out, "Image updates: fetching versions", func() {
				items, held, warns = splitCandidates(entries, only, defAge, now, res)
			})
			for _, w := range warns {
				fmt.Fprintln(out, yellow("! "+w))
			}
			printHeld(out, held, now)
			if len(items) == 0 {
				if len(held) > 0 {
					fmt.Fprintln(out, "Nothing to update (every candidate is locked).")
					return nil
				}
				fmt.Fprintln(out, "Everything up to date.")
				return nil
			}

			var chosen []int
			if yes {
				// Non-interactive: aged candidates, plus anything explicitly
				// named (naming is the young-override).
				for k, it := range items {
					if !it.c.Young || only[it.e.Key] {
						chosen = append(chosen, k)
					}
				}
			} else {
				// Everything starts unselected: applying an update is an
				// explicit choice, not an opt-out. Named entries are the
				// exception (naming them was the choice).
				opts := make([]huh.Option[int], len(items))
				for k, it := range items {
					opts[k] = huh.NewOption(it.label(), k).Selected(only[it.e.Key])
				}
				err := huh.NewForm(huh.NewGroup(
					huh.NewMultiSelect[int]().
						Title("Image updates").
						Description("space toggles, enter applies; ! items are younger than min-age").
						Options(opts...).
						Value(&chosen),
				)).Run()
				if errors.Is(err, huh.ErrUserAborted) {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
				if err != nil {
					return fmt.Errorf("interactive selection unavailable (%v); use -y or name entries", err)
				}
			}
			if len(chosen) == 0 {
				fmt.Fprintln(out, "Nothing selected.")
				return nil
			}

			for _, k := range chosen {
				it := items[k]
				r, _ := registry.Parse(it.e.Image)
				entries[it.idx].Image = r.Repo + ":" + it.c.Tag
				entries[it.idx].Digest = it.c.Digest
				fmt.Fprintf(out, "  %s %s: %s -> %s\n", green("~"), it.e.Key, it.c.Reason, short(it.c.Digest))
			}
			content, err := emitImageRegistry(entries)
			if err != nil {
				return err
			}
			path := filepath.Join(projectDir, "registries", "images.cue")
			if err := os.WriteFile(path, content, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			fmt.Fprintf(out, "\nWrote %d update(s). Run 'crei plan' to see the change.\n", len(chosen))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the picker; apply all aged candidates")
	cmd.Flags().StringVar(&minAgeFlag, "min-age", "", "mark candidates younger than this (e.g. 7d) and exclude them from -y; per-entry minAge overrides")
	return cmd
}
