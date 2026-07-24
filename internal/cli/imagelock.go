package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
)

// lockDateFormat is the schema's since format (and what the regex in
// #ImageEntry.lock validates).
const lockDateFormat = "2006-01-02"

// lockDate renders when the lock was set: "today" for one set today (since
// carries a date, not a timestamp, so sub-day precision is not real), the raw
// YYYY-MM-DD otherwise, and "" when since is unset or hand-edited to garbage.
// The raw date, not a fuzzy age, is the decay signal a stale lock needs: a
// 2026-01 date next to a reason about a bug fixed in March tells the story.
// The clock is a parameter so a caller with its own reference time (the
// outdated report) stays consistent with itself.
func lockDate(l *eval.ImageLock, at time.Time) string {
	age, ok := lockAge(l, at)
	switch {
	case !ok:
		return ""
	case age < 24*time.Hour:
		return "today"
	default:
		return l.Since
	}
}

// lockAge is how long the lock has been in place, false when since is unset or
// unparseable (hand-edited). A future date yields a zero age rather than a
// negative one, so a typo reads as "just placed" instead of nonsense.
func lockAge(l *eval.ImageLock, at time.Time) (time.Duration, bool) {
	if l.Since == "" {
		return 0, false
	}
	t, err := time.Parse(lockDateFormat, l.Since)
	if err != nil {
		return 0, false
	}
	if d := at.Sub(t); d > 0 {
		return d, true
	}
	return 0, true
}

// writeImages regenerates registries/images.cue from entries.
func writeImages(projectDir string, entries []eval.ImageEntry) error {
	content, err := emitImageRegistry(entries)
	if err != nil {
		return err
	}
	path := filepath.Join(projectDir, "registries", "images.cue")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// findImage returns the index of the named entry, or an error naming what is
// available so a typo fails loudly instead of silently locking nothing.
func findImage(entries []eval.ImageEntry, name string) (int, error) {
	var available []string
	for i := range entries {
		if entries[i].Key == name {
			return i, nil
		}
		available = append(available, entries[i].Key)
	}
	return -1, fmt.Errorf("no image named %q in the registry (available: %s)", name, strings.Join(available, ", "))
}

func newImageLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock <name> <reason...>",
		Short: "Hold an image at its current pin, recording why",
		Long: "lock holds an entry where it is and records the reason next to it in\n" +
			"registries/images.cue. While locked, crei rewrites nothing about the\n" +
			"entry: update will not offer it, pin skips it, and add --force refuses.\n\n" +
			"The reason is required and is shown by update and outdated whenever the\n" +
			"entry has an update waiting, so the rationale outlives your memory of\n" +
			"it. Naming a locked entry on the update command line does not override\n" +
			"the lock (a min-age marker can be overridden that way; a lock cannot) —\n" +
			"run 'crei image unlock' when the reason no longer holds.\n\n" +
			"Stronger than range: \"=x.y.z\", which freezes the tag but lets the\n" +
			"digest follow a re-push. A lock freezes both.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, reason := args[0], strings.TrimSpace(strings.Join(args[1:], " "))
			if reason == "" {
				return fmt.Errorf("a lock needs a reason: crei image lock %s <why>", name)
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
			i, err := findImage(entries, name)
			if err != nil {
				return err
			}
			if prev := entries[i].Lock; prev != nil {
				fmt.Fprintf(out, "%s %s was already locked: %s\n", yellow("!"), name, prev.Reason)
			}
			entries[i].Lock = &eval.ImageLock{Reason: reason, Since: time.Now().Format(lockDateFormat)}
			// Read what is being reported before writing: the entry is what the
			// index points at now, not after any reordering the writer does.
			image, pin := entries[i].Image, entries[i].Digest
			if pin == "" {
				pin = "unpinned"
			} else {
				pin = short(pin)
			}
			if err := writeImages(projectDir, entries); err != nil {
				return err
			}
			fmt.Fprintf(out, "%s %s locked at %s (%s)\n  %s\n", green("+"), name, image, pin, reason)
			return nil
		},
	}
	return cmd
}

func newImageUnlockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlock <name...>",
		Short: "Release a lock so the entry can be updated again",
		Long: "unlock clears the hold placed by 'crei image lock', letting update\n" +
			"offer the entry again. It changes no pin by itself: run\n" +
			"'crei image update' afterwards to actually move it.",
		Args: cobra.MinimumNArgs(1),
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
			cleared := 0
			for _, name := range args {
				i, err := findImage(entries, name)
				if err != nil {
					return err
				}
				if entries[i].Lock == nil {
					fmt.Fprintf(out, "  %s %s (not locked)\n", dim("-"), name)
					continue
				}
				fmt.Fprintf(out, "  %s %s unlocked (was: %s)\n", green("~"), name, entries[i].Lock.Reason)
				entries[i].Lock = nil
				cleared++
			}
			if cleared == 0 {
				fmt.Fprintln(out, "Nothing to unlock.")
				return nil
			}
			if err := writeImages(projectDir, entries); err != nil {
				return err
			}
			fmt.Fprintf(out, "\nCleared %d lock(s). Run 'crei image update' to move them.\n", cleared)
			return nil
		},
	}
	return cmd
}
