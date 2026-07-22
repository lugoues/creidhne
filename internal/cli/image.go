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
	cmd.AddCommand(newImageOutdatedCmd(), newImagePinCmd())
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
			"than the min-age (per-entry minAge, else --min-age) is held, not\n" +
			"reported as available. Read-only; exits non-zero when updates are\n" +
			"available.",
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
			rows, available := checkOutdated(entries, defAge, time.Now(), liveResolver())
			printImageRows(out, rows)
			if available > 0 {
				return errSilent{}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&minAgeFlag, "min-age", "", "hold updates younger than this (e.g. 7d); per-entry minAge overrides")
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
}

func liveResolver() resolver {
	return resolver{digest: registry.Digest, created: registry.Created}
}

// checkOutdated resolves each managed entry and classifies it. Returns the
// rows and how many have an available (non-held) update.
func checkOutdated(entries []eval.ImageEntry, defAge time.Duration, now time.Time, res resolver) ([]imageRow, int) {
	var rows []imageRow
	available := 0
	for _, e := range entries {
		r, err := registry.Parse(e.Image)
		if err != nil {
			rows = append(rows, imageRow{name: e.Key, status: "invalid", note: err.Error()})
			continue
		}
		status := registry.Classify(r.Tag != "", e.Digest != "")
		row := imageRow{name: e.Key, status: string(status)}
		switch status {
		case registry.Unpinned:
			row.note = "no digest — run 'crei image pin'"
		case registry.Unmanaged:
			row.note = "no tag — can't check for updates"
		case registry.Managed:
			cur, err := res.digest(r.TaggedRef())
			if err != nil {
				row.note = "lookup failed: " + firstLine(err.Error())
				break
			}
			if cur == e.Digest {
				row.note = "up to date"
				break
			}
			effAge := defAge
			if e.MinAge != "" {
				effAge, _ = registry.ParseAge(e.MinAge) // schema regex already validated
			}
			if effAge > 0 {
				if created, err := res.created(r.Repo + "@" + cur); err == nil {
					if age := now.Sub(created); age < effAge {
						row.note = fmt.Sprintf("held: candidate is %s old (min-age %s)", humanDuration(age), humanDuration(effAge))
						break
					}
				}
			}
			row.update = true
			row.note = "update available: " + short(cur)
			available++
		}
		rows = append(rows, row)
	}
	return rows, available
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
