package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/reconcile"
)

func newRenderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "render",
		Short: "Render all quadlet files to stdout",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			desired, err := generate(cfg.ProjectDir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			names := sortedKeys(desired)
			fmt.Fprintf(out, "# %d quadlet file(s)\n\n", len(names))
			for _, n := range names {
				fmt.Fprintf(out, "--- %s ---\n", n)
				_, _ = out.Write(desired[n].Content)
				fmt.Fprintln(out)
			}
			return nil
		},
	}
}

func newPlanCmd() *cobra.Command {
	var noDiff bool
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what apply would do without making changes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			desired, err := generate(cfg.ProjectDir)
			if err != nil {
				return err
			}
			changes, err := reconcile.ComputePlan(desired, cfg.QuadletDir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if err := renderPlan(out, changes, cfg, noDiff); err != nil {
				return err
			}
			s := reconcile.Summarize(changes)
			printSummary(out, s, "to add", "to update", "to remove")
			if s.Added == 0 && s.Changed == 0 && s.Removed == 0 {
				fmt.Fprintln(out, "Nothing to do.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&noDiff, "no-diff", false, "show only the change list, not inline diffs")
	return cmd
}

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Diff generated files against the live quadlet directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			desired, err := generate(cfg.ProjectDir)
			if err != nil {
				return err
			}
			changes, err := reconcile.ComputePlan(desired, cfg.QuadletDir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if err := printDiff(out, changes, cfg.QuadletDir, cfg.DiffTool); err != nil {
				return err
			}
			printSummary(out, reconcile.Summarize(changes), "new", "changed", "stale")
			return nil
		},
	}
}

func newApplyCmd() *cobra.Command {
	var reloadSystemd, yes, noDiff bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Write generated quadlet files to the quadlet directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			desired, err := generate(cfg.ProjectDir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			dir := cfg.QuadletDir
			changes, err := reconcile.ComputePlan(desired, dir)
			if err != nil {
				return err
			}
			s := reconcile.Summarize(changes)
			if err := renderPlan(out, changes, cfg, noDiff); err != nil {
				return err
			}
			printSummary(out, s, "to add", "to update", "to remove")
			if s.Added == 0 && s.Changed == 0 && s.Removed == 0 {
				fmt.Fprintln(out, "Nothing to do.")
				return nil
			}
			if !yes {
				ok, err := confirm(cmd.InOrStdin(), out, "Apply changes?")
				if err != nil {
					// No answer available (cron, CI, piped/redirected stdin):
					// fail loudly rather than silently aborting with a success
					// exit code that looks like a completed apply.
					return err
				}
				if !ok {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
			}
			// Apply removals before writes so a path that changes file<->directory
			// shape (e.g. a build context that became a directory) is cleared
			// first, and a stale-file removal can never os.RemoveAll a freshly
			// written file under a shared prefix.
			applyPass := func(remove bool) error {
				for _, c := range changes {
					var werr error
					p := filepath.Join(dir, filepath.FromSlash(c.Name))
					switch c.Action {
					case reconcile.ActionRemove:
						if !remove {
							continue
						}
						werr = reconcile.RemoveFile(p)
					case reconcile.ActionAdd, reconcile.ActionChange:
						if remove {
							continue
						}
						werr = reconcile.WriteFile(p, c.Content, c.Mode)
					default:
						continue
					}
					if werr != nil {
						if os.IsPermission(werr) {
							return fmt.Errorf("permission denied writing to %s\n  re-run with elevated privileges, e.g.: sudo crei apply --quadlet-dir %q", dir, dir)
						}
						return werr
					}
				}
				return nil
			}
			if err := applyPass(true); err != nil {
				return err
			}
			if err := applyPass(false); err != nil {
				return err
			}
			if err := reconcile.PruneEmptyDirs(filepath.Join(dir, "images")); err != nil {
				return err
			}
			fmt.Fprintf(out, "\nApplied: %d added, %d updated, %d removed\n", s.Added, s.Changed, s.Removed)
			userScope := underHome(dir)
			// Reload default comes from crei.toml (reload_systemd, default on to
			// match podman quadlet install); an explicit --reload-systemd flag
			// overrides it for this run.
			reload := cfg.ReloadSystemd
			if cmd.Flags().Changed("reload-systemd") {
				reload = reloadSystemd
			}
			if reload {
				if err := reconcile.DaemonReload(userScope); err != nil {
					return fmt.Errorf("daemon-reload: %w", err)
				}
				fmt.Fprintln(out, "Daemon reloaded.")
			} else {
				fmt.Fprintf(out, "Run '%s' to pick up changes.\n", reconcile.ReloadHint(userScope))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&reloadSystemd, "reload-systemd", false, "run systemctl daemon-reload after applying (default: reload_systemd in crei.toml, else on)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.Flags().BoolVar(&noDiff, "no-diff", false, "show only the change list, not inline diffs")
	return cmd
}

// renderPlan prints the change set: an inline diff per change (Terraform-style)
// by default, or just the +/~/- change list when noDiff is set.
func renderPlan(w io.Writer, changes []reconcile.Change, cfg config, noDiff bool) error {
	if noDiff {
		printChangeList(w, changes)
		return nil
	}
	return printDiff(w, changes, cfg.QuadletDir, cfg.DiffTool)
}

// printChangeList prints one +/~/- line per change (the compact view).
func printChangeList(w io.Writer, changes []reconcile.Change) {
	for _, c := range changes {
		switch c.Action {
		case reconcile.ActionAdd:
			fmt.Fprintln(w, green("+ "+c.Name))
		case reconcile.ActionChange:
			fmt.Fprintln(w, yellow("~ "+c.Name))
		case reconcile.ActionRemove:
			fmt.Fprintln(w, red("- "+c.Name))
		case reconcile.ActionUnchanged:
			fmt.Fprintln(w, dim("  "+c.Name))
		}
	}
}

// printDiff renders each change as an inline diff, Terraform-style: a new file
// shows its content as added lines, a removed file shows the (live) content as
// removed lines, and a changed file shows a unified diff. Unchanged files are
// omitted; the summary reports their count. Entries are separated by one blank.
func printDiff(w io.Writer, changes []reconcile.Change, quadletDir, diffTool string) error {
	printed := false
	for _, c := range changes {
		if c.Action == reconcile.ActionUnchanged {
			continue
		}
		if printed {
			fmt.Fprintln(w)
		}
		printed = true
		switch c.Action {
		case reconcile.ActionAdd:
			fmt.Fprintln(w, green("+ "+c.Name))
			writeBodyLines(w, c.Content, "+", green)
		case reconcile.ActionChange:
			fmt.Fprintln(w, yellow("~ "+c.Name))
			live := filepath.Join(quadletDir, filepath.FromSlash(c.Name))
			d, err := reconcile.RunDiff(live, c.Content, "live/"+c.Name, "new/"+c.Name, diffTool)
			if err != nil {
				return err
			}
			if diffTool == "" || diffTool == "diff" {
				d = colorizeDiff(d)
			}
			for _, line := range splitLines(d) {
				fmt.Fprintln(w, "  "+line)
			}
		case reconcile.ActionRemove:
			fmt.Fprintln(w, red("- "+c.Name))
			// The change carries no content for removals; read what's on disk so
			// the user sees what is about to be deleted.
			if body, err := os.ReadFile(filepath.Join(quadletDir, filepath.FromSlash(c.Name))); err == nil {
				writeBodyLines(w, body, "-", red)
			}
		}
	}
	return nil
}

// writeBodyLines prints each line of content, prefixed with sign ("+"/"-"),
// colored and indented under its file header. Leading and trailing blank lines
// (rendered unit files start with one) are dropped.
func writeBodyLines(w io.Writer, content []byte, sign string, color func(string) string) {
	lines := splitLines(string(content))
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		fmt.Fprintln(w, color("  "+sign+" "+line))
	}
}

func splitLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func printSummary(w io.Writer, s reconcile.Summary, addVerb, changeVerb, removeVerb string) {
	fmt.Fprintf(w, "\n%d file(s): %d %s, %d %s, %d unchanged, %d %s\n",
		s.Total, s.Added, addVerb, s.Changed, changeVerb, s.Unchanged, s.Removed, removeVerb)
}

// colorizeDiff colors the built-in unified diff (difflib emits none). External
// tools are assumed to color their own output. The lipgloss-backed color
// helpers render plain when color is unavailable, so no useColor guard is needed.
func colorizeDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			// file headers: leave uncolored
		case strings.HasPrefix(line, "+"):
			lines[i] = green(line)
		case strings.HasPrefix(line, "-"):
			lines[i] = red(line)
		case strings.HasPrefix(line, "@@"):
			lines[i] = dim(line)
		}
	}
	return strings.Join(lines, "\n")
}
