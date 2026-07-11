package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/reconcile"
)

func newRenderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "render [quadlet...]",
		Short: "Render quadlet files to stdout (all quadlets, or only the named ones)",
		Long: "render prints the generated unit files to stdout. With no arguments it\n" +
			"renders every quadlet in the project; given one or more quadlet names it\n" +
			"renders only those (a subset renders identically, since cross-quadlet\n" +
			"references are resolved at eval time).",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			quads, err := loadQuadlets(cfg.ProjectDir)
			if err != nil {
				return err
			}
			if len(quads) == 0 {
				return fmt.Errorf("no quadlets found (no top-level #Quadlet values in %s)", cfg.ProjectDir)
			}
			if len(args) > 0 {
				quads, err = filterQuadlets(quads, args)
				if err != nil {
					return err
				}
			}
			desired, err := renderQuadlets(quads)
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
			if err := printDiff(out, changes, cfg.QuadletDir, cfg.DiffTool, cfg.DiffStyle); err != nil {
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
			quads, err := loadQuadlets(cfg.ProjectDir)
			if err != nil {
				return err
			}
			if len(quads) == 0 {
				return fmt.Errorf("no quadlets found (no top-level #Quadlet values in %s)", cfg.ProjectDir)
			}
			desired, err := renderQuadlets(quads)
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
				// Adopt/refresh recorded state even when the files are already
				// current, so deployments from before crei.state gain one on
				// their next apply. Best-effort: no file writes were needed, so
				// a read-only dir must not turn a no-op into a failure.
				if err := recordState(out, dir, quads, false); err != nil {
					return err
				}
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
						// errors.Is (not os.IsPermission) so the check still sees a
						// permission error through reconcile.WriteFile's %w wrap.
						if errors.Is(werr, fs.ErrPermission) {
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
			// Record what was just applied (manifest + per-file hashes). We
			// could write the files, so failing to record them is a real error.
			if err := recordState(out, dir, quads, true); err != nil {
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
	return printDiff(w, changes, cfg.QuadletDir, cfg.DiffTool, cfg.DiffStyle)
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

// printDiff renders each change as an inline diff, Terraform-style: a bold "#
// name" header, then a new file's content as added lines, a removed file's (live)
// content as removed lines, and a changed file as a structured inline diff.
// Unchanged files are omitted; the summary reports their count. Entries are
// separated by one blank line.
func printDiff(w io.Writer, changes []reconcile.Change, quadletDir, diffTool, diffStyle string) error {
	printed := false
	for _, c := range changes {
		if c.Action == reconcile.ActionUnchanged {
			continue
		}
		if printed {
			fmt.Fprintln(w)
		}
		printed = true
		fmt.Fprintln(w, diffHeaderStyle.Render("# "+c.Name))
		switch c.Action {
		case reconcile.ActionAdd:
			for _, l := range bodyLines(c.Content) {
				bodyln(w, green("+ "+l))
			}
		case reconcile.ActionRemove:
			// The change carries no content for removals; read what's on disk so
			// the user sees what is about to be deleted.
			body, _ := os.ReadFile(filepath.Join(quadletDir, filepath.FromSlash(c.Name)))
			for _, l := range bodyLines(body) {
				bodyln(w, red("- "+l))
			}
		case reconcile.ActionChange:
			// Built-in differ: render a structured inline diff from the in-memory
			// old/new content. A configured external tool formats (and colors) its
			// own output, so pass it through indented.
			if diffTool == "" || diffTool == "diff" {
				renderInlineDiff(w, c.Existing, c.Content, diffStyle)
			} else {
				live := filepath.Join(quadletDir, filepath.FromSlash(c.Name))
				d, err := reconcile.RunDiff(live, c.Content, "live/"+c.Name, "new/"+c.Name, diffTool)
				if err != nil {
					return err
				}
				for _, l := range splitLines(d) {
					bodyln(w, l)
				}
			}
		}
	}
	return nil
}

// bodyLines splits content into lines, dropping leading and trailing blank lines
// (rendered unit files start with one).
func bodyLines(content []byte) []string {
	lines := splitLines(string(content))
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func splitLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func printSummary(w io.Writer, s reconcile.Summary, addVerb, changeVerb, removeVerb string) {
	fmt.Fprintf(w, "\n%d file(s): %d %s, %d %s, %d unchanged, %d %s\n",
		s.Total, s.Added, addVerb, s.Changed, changeVerb, s.Unchanged, s.Removed, removeVerb)
}
