package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/registry"
)

func newImageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "image",
		Short: "Manage the image registry: pin digests and report updates",
		Long: "image works on the project's registries/images.cue — the crei-owned\n" +
			"source of truth for external images. Each managed entry tracks a tag and\n" +
			"pins a digest (repo:tag@sha256:...); podman pulls the digest, crei checks\n" +
			"the tag for updates. Bumping is a config write-back, not a runtime pull.",
	}
	cmd.AddCommand(newImageAddCmd(), newImageOutdatedCmd(), newImagePinCmd(), newImageUpdateCmd(),
		newImageLockCmd(), newImageUnlockCmd())
	return cmd
}

// loadImages loads the project's image registry with the schema overlay.
func loadImages() ([]eval.ImageEntry, string, error) {
	cfg, err := resolveConfig()
	if err != nil {
		return nil, "", err
	}
	overlay, err := buildOverlay(cfg.ProjectDir)
	if err != nil {
		return nil, "", err
	}
	entries, err := eval.LoadImageRegistry(cfg.ProjectDir, overlay)
	return entries, cfg.ProjectDir, err
}

func newImageOutdatedCmd() *cobra.Command {
	var minAgeFlag string
	cmd := &cobra.Command{
		Use:   "outdated",
		Short: "Report managed images whose tracked tag has a newer digest",
		Long: "outdated resolves each managed entry's tag to its current registry\n" +
			"digest and reports the ones whose pin is behind. A candidate younger\n" +
			"than the min-age (per-entry minAge, else --min-age) is still reported,\n" +
			"marked '! younger than min-age' (min-age is information, not a gate).\n" +
			"A locked entry is reported as locked and never counts as an available\n" +
			"update. Read-only; exits non-zero when an update is available.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			defAge, err := registry.ParseAge(minAgeFlag)
			if err != nil {
				return err
			}
			entries, _, err := loadImages()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(entries) == 0 {
				fmt.Fprintln(out, "No image registry (registries/images.cue).")
				return nil
			}
			var rows []imageRow
			available, failures := 0, 0
			withSpinner(out, "Checking registries", func() {
				rows, available, failures = checkOutdated(entries, defAge, time.Now(), liveResolver())
			})
			printImageRows(out, rows)
			// Failed lookups exit non-zero on their own: an outage or auth
			// failure must be distinguishable from "everything current" in CI.
			if failures > 0 {
				return fmt.Errorf("%d lookup failure(s); those entries' update state is unknown", failures)
			}
			if available > 0 {
				return errSilent{}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&minAgeFlag, "min-age", "", "mark updates younger than this (e.g. 7d); per-entry minAge overrides")
	return cmd
}

// imageRow is one line of the outdated report.
type imageRow struct {
	name   string
	status string // managed / unpinned / unmanaged
	note   string
	update bool // a newer digest is available (not held)
}

// resolver abstracts the registry queries so checkOutdated is testable without
// network. Defaults wired in the command; tests inject fakes.
type resolver struct {
	digest  func(repoTag string) (string, error)
	created func(ref string) (time.Time, error)
	tags    func(repo string) ([]string, error)
}

func liveResolver() resolver {
	return resolver{digest: registry.Digest, created: registry.Created, tags: registry.Tags}
}

// checkOutdated resolves each managed entry and classifies it. Returns the
// rows, how many have an available (non-held) update, and how many lookups
// failed (their update state is unknown, which callers must not report as
// "everything current").
func checkOutdated(entries []eval.ImageEntry, defAge time.Duration, now time.Time, res resolver) ([]imageRow, int, int) {
	var rows []imageRow
	available, failures := 0, 0
	for _, e := range entries {
		r, err := registry.Parse(e.Image)
		if err != nil {
			rows = append(rows, imageRow{name: e.Key, status: "invalid", note: err.Error()})
			continue
		}
		status := registry.Classify(r.Tag != "", e.Digest != "")
		row := imageRow{name: e.Key, status: string(status)}
		// A lock is reported instead of the update, but the candidate is still
		// resolved and named: the point of the report is to show what the hold
		// is costing, not to hide it. Held updates are not "available", so they
		// never make outdated exit non-zero.
		if e.Lock != nil {
			// The status column already reads "locked", so the note is just the
			// reason (dated when known) and what the lock is holding back.
			row.status = "locked"
			row.note = e.Lock.Reason
			if d := lockDate(e.Lock, now); d != "" {
				row.note += " (" + d + ")"
			}
			if status == registry.Managed {
				if c, err := nextPin(e, r, defAge, now, res); err == nil && c.Reason != "" {
					row.note += "; holding back " + c.Reason + " " + short(c.Digest)
				} else if err != nil {
					// A lock excludes the entry from available updates, but a
					// failed lookup is still a failed lookup: without counting
					// it, an outage on locked entries exits 0 as "current".
					row.note += "; lookup failed: " + firstLine(err.Error())
					failures++
				}
			}
			rows = append(rows, row)
			continue
		}
		switch status {
		case registry.Unpinned:
			row.note = "no digest — run 'crei image pin'"
		case registry.Unmanaged:
			row.note = "no tag — can't check for updates"
		case registry.Managed:
			c, err := nextPin(e, r, defAge, now, res)
			if err != nil {
				row.note = "lookup failed: " + firstLine(err.Error())
				failures++
				break
			}
			if c.Reason == "" {
				row.note = "up to date"
				break
			}
			row.update = true
			row.note = "update available: " + c.Reason + " " + short(c.Digest)
			if c.HasAge {
				row.note += " (released " + humanDuration(c.Age) + " ago)"
			}
			if c.Young {
				row.note += " ! younger than min-age"
			}
			available++
		}
		rows = append(rows, row)
	}
	return rows, available, failures
}

// checkImageNames rejects requested entry names that don't exist in the
// registry: a typo'd `crei image pin typo` exiting 0 with "Nothing to pin"
// reads as success while doing nothing.
func checkImageNames(entries []eval.ImageEntry, only map[string]bool) error {
	if len(only) == 0 {
		return nil
	}
	known := map[string]bool{}
	available := make([]string, 0, len(entries))
	for _, e := range entries {
		known[e.Key] = true
		available = append(available, e.Key)
	}
	var unknown []string
	for name := range only {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	sort.Strings(available)
	if len(available) == 0 {
		return fmt.Errorf("no registry entr(y/ies) named %s (the registry is empty; add entries with crei image add)", strings.Join(unknown, ", "))
	}
	return fmt.Errorf("no registry entr(y/ies) named %s (available: %s)", strings.Join(unknown, ", "), strings.Join(available, ", "))
}

func short(digest string) string {
	if i := strings.IndexByte(digest, ':'); i >= 0 && len(digest) > i+13 {
		return digest[:i+13]
	}
	return digest
}

func printImageRows(out io.Writer, rows []imageRow) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	nameW, statusW := 0, 0
	for _, r := range rows {
		if len(r.name) > nameW {
			nameW = len(r.name)
		}
		if len(r.status) > statusW {
			statusW = len(r.status)
		}
	}
	for _, r := range rows {
		// Style after padding so ANSI codes never skew the columns.
		status := pad(r.status, statusW)
		if r.update {
			status = yellow(status)
		} else {
			status = dim(status)
		}
		fmt.Fprintf(out, "%s  %s  %s\n", pad(r.name, nameW), status, r.note)
	}
}

func pad(s string, w int) string { return s + strings.Repeat(" ", w-len(s)) }
