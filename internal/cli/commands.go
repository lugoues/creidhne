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
	return &cobra.Command{
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
			printChanges(out, changes, true)
			s := reconcile.Summarize(changes)
			printSummary(out, s, "to add", "to update", "to remove")
			if s.Added == 0 && s.Changed == 0 && s.Removed == 0 {
				fmt.Fprintln(out, "Nothing to do.")
			}
			return nil
		},
	}
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
			for _, c := range changes {
				switch c.Action {
				case reconcile.ActionAdd:
					fmt.Fprintln(out, green("+ "+c.Name+" (new)"))
					for _, line := range strings.Split(strings.TrimRight(string(c.Content), "\n"), "\n") {
						fmt.Fprintln(out, "  "+line)
					}
					fmt.Fprintln(out)
				case reconcile.ActionChange:
					fmt.Fprintln(out, yellow("~ "+c.Name+" (changed)"))
					live := filepath.Join(cfg.QuadletDir, filepath.FromSlash(c.Name))
					diffOut, err := reconcile.RunDiff(live, c.Content, "live/"+c.Name, "new/"+c.Name, cfg.DiffTool)
					if err != nil {
						return err
					}
					if cfg.DiffTool == "" || cfg.DiffTool == "diff" {
						diffOut = colorizeDiff(diffOut)
					}
					fmt.Fprint(out, diffOut)
					fmt.Fprintln(out)
				case reconcile.ActionRemove:
					fmt.Fprintln(out, red("- "+c.Name+" (stale, will be removed)"))
				}
			}
			printSummary(out, reconcile.Summarize(changes), "new", "changed", "stale")
			return nil
		},
	}
}

func newApplyCmd() *cobra.Command {
	var reloadSystemd, yes bool
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
			printChanges(out, changes, false)
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
	return cmd
}

// printChanges prints the +/~/- change lines. withVerb adds the
// (add)/(update)/(remove) suffixes used by `plan`; `apply` passes false.
func printChanges(w io.Writer, changes []reconcile.Change, withVerb bool) {
	for _, c := range changes {
		switch c.Action {
		case reconcile.ActionAdd:
			line := "  + " + c.Name
			if withVerb {
				line += " (add)"
			}
			fmt.Fprintln(w, green(line))
		case reconcile.ActionChange:
			line := "  ~ " + c.Name
			if withVerb {
				line += " (update)"
			}
			fmt.Fprintln(w, yellow(line))
		case reconcile.ActionUnchanged:
			fmt.Fprintln(w, dim("    "+c.Name+" (unchanged)"))
		case reconcile.ActionRemove:
			line := "  - " + c.Name
			if withVerb {
				line += " (remove)"
			}
			fmt.Fprintln(w, red(line))
		}
	}
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
