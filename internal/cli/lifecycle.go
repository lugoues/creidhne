package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/lugoues/creidhne/internal/systemd"
)

// restartPoll is how often trackedRestart re-checks the systemd job queue, and
// restartSleep the wait itself; the systemd calls are indirected too. All are
// package vars so tests can drive the poll loop without a real systemctl or
// real delays.
var (
	restartPoll     = 400 * time.Millisecond
	restartSleep    = time.Sleep
	restartAsyncFn  = systemd.RestartAsync
	pendingJobsFn   = systemd.PendingJobs
	restartStatusFn = systemd.Show
)

// trackedRestart enqueues a non-blocking restart (--no-block, so systemd still
// orders the jobs by their dependencies) and then polls the job queue, checking
// off each unit as its job clears. It turns systemctl restart's silent,
// multi-minute wait into a live checklist that also reveals which unit is the
// straggler, then confirms final state and reports anything that came back
// failed.
func trackedRestart(out io.Writer, userScope bool, units []string) error {
	if len(units) == 0 {
		return nil
	}
	if err := restartAsyncFn(userScope, units); err != nil {
		return err
	}
	remaining := make(map[string]bool, len(units))
	for _, u := range units {
		remaining[u] = true
	}
	spin := newRestartSpinner(out) // animates only on a terminal
	for len(remaining) > 0 {
		pending, err := pendingJobsFn(userScope, units)
		if err != nil {
			spin.clear()
			return err
		}
		// Check off completions in the stable input order, not map order.
		for _, u := range units {
			if remaining[u] {
				if _, still := pending[u]; !still {
					spin.clear() // lift the spinner line before printing above it
					fmt.Fprintf(out, "  %s %s\n", green("✓"), u)
					delete(remaining, u)
				}
			}
		}
		if len(remaining) == 0 {
			break
		}
		// Spin through one poll interval so a slow unit reads as working, not
		// hung, and names what is still pending.
		spin.wait(restartPoll, waitingLabel(units, remaining))
	}
	spin.clear()
	// A cleared job is not proof of success; name anything that ended failed.
	var failed []string
	if statuses, err := restartStatusFn(userScope, units); err == nil {
		for _, u := range units {
			if st, ok := statuses[u]; ok && st.ActiveState == "failed" {
				failed = append(failed, u)
			}
		}
	}
	if len(failed) > 0 {
		for _, u := range failed {
			fmt.Fprintf(out, "  %s %s failed\n", red("✗"), u)
		}
		return fmt.Errorf("%d unit(s) failed to restart: %s", len(failed), strings.Join(failed, ", "))
	}
	fmt.Fprintf(out, "%s Restarted.\n", green("✓"))
	return nil
}

// spinFrames is the braille cycle huh's spinner uses; reused so restart's
// hand-rolled spinner matches the rest of the CLI.
var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinInterval is the animation tick (a poll interval spans several frames);
// a package var so a test could drive it without real time.
var spinInterval = 100 * time.Millisecond

// restartSpinner draws an in-place "waiting on …" line during the poll gaps.
// It animates only to a terminal; off a terminal (pipe, CI, tests) it is inert
// and the plain per-unit checklist stands on its own.
type restartSpinner struct {
	w      io.Writer
	active bool
	frame  int
}

func newRestartSpinner(out io.Writer) *restartSpinner {
	active := false
	if f, ok := out.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		active = true
	}
	return &restartSpinner{w: out, active: active}
}

// clear erases the current spinner line so text can be printed above it.
func (s *restartSpinner) clear() {
	if s.active {
		fmt.Fprint(s.w, "\r\033[K")
	}
}

// wait animates the spinner for d, labelling what it is waiting on. When inert
// it just sleeps the whole interval once (the poll cadence).
func (s *restartSpinner) wait(d time.Duration, label string) {
	if !s.active {
		restartSleep(d)
		return
	}
	for waited := time.Duration(0); waited < d; waited += spinInterval {
		fmt.Fprintf(s.w, "\r\033[K%s restarting, waiting on %s", green(spinFrames[s.frame%len(spinFrames)]), label)
		s.frame++
		restartSleep(spinInterval)
	}
}

// waitingLabel names the units still pending, in input order, capping the list
// so a wide fan-out stays one line.
func waitingLabel(units []string, remaining map[string]bool) string {
	var names []string
	for _, u := range units {
		if remaining[u] {
			names = append(names, u)
		}
	}
	const shown = 3
	if len(names) > shown {
		return fmt.Sprintf("%s +%d more", strings.Join(names[:shown], ", "), len(names)-shown)
	}
	return strings.Join(names, ", ")
}

// stdinIsTTY reports whether in is an interactive terminal (the same check
// confirm uses to pick prompt style).
func stdinIsTTY(in io.Reader) bool {
	f, ok := in.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// staleOptionLabel renders a picker entry: bold unit name, the staleness
// delta beneath it (the update picker's multiline shape).
func staleOptionLabel(r statusRow) string {
	note := "stale"
	if r.StaleNote != "" {
		note = "stale: " + r.StaleNote
	}
	return fmt.Sprintf("%s\n  %s", bold(r.Service), note)
}

// lifecycleRows selects the units acted on by restart/logs: every unit of
// the named quadlets, narrowed to stale ones by staleOnly. skipUnrestartable
// drops (with a warning) stale units whose change a restart cannot apply.
// Rows are returned (not just service names) so callers can show why a unit
// is in the set (the staleness delta). pending counts files whose desired
// content differs from disk: staleness only begins at apply, so a
// nothing-to-restart answer can explain an update-but-no-apply workflow gap.
func lifecycleRows(out io.Writer, cfg config, args []string, staleOnly, skipUnrestartable bool) (rows []statusRow, pending int, err error) {
	in, notes, err := gatherStatus(cfg, args)
	if err != nil {
		return nil, 0, err
	}
	for _, n := range notes {
		fmt.Fprintln(out, yellow("! "+n))
	}
	for _, r := range classifyRows(in) {
		if r.Disk == diskPending || r.Disk == diskMissing {
			pending++
		}
		if r.Service == "" {
			continue
		}
		if staleOnly {
			if !r.Stale {
				continue
			}
			if skipUnrestartable {
				rec, _ := in.Recorded.FileRecord(r.Path)
				if hint := restartHint(r.Path, rec); hint != "" {
					fmt.Fprintln(out, yellow("! skipping "+hint))
					continue
				}
			}
		}
		rows = append(rows, r)
	}
	return rows, pending, nil
}

// serviceNames extracts the systemd unit names from lifecycle rows.
func serviceNames(rows []statusRow) []string {
	units := make([]string, 0, len(rows))
	for _, r := range rows {
		units = append(units, r.Service)
	}
	return units
}

func newRestartCmd() *cobra.Command {
	var staleOnly, yes bool
	cmd := &cobra.Command{
		Use:   "restart [quadlet...]",
		Short: "Restart quadlet units via systemctl (--stale: only units running outdated config)",
		Long: "restart restarts the named quadlets' units via systemctl, in the scope\n" +
			"the quadlet directory implies (user when under $HOME, system otherwise).\n\n" +
			"--stale restricts the set to units whose running process predates the\n" +
			"last applied config change (what status flags as stale), making the\n" +
			"applied changes take effect. Stale units whose change a restart cannot\n" +
			"apply (volumes; networks without NetworkDeleteOnStop) are skipped with\n" +
			"a warning; see 'crei diff --stale' for what each restart would change.\n\n" +
			"The restart runs as one ordered systemctl transaction (dependencies are\n" +
			"honored); crei then tracks the job queue and checks off each unit as it\n" +
			"finishes, so a slow unit is visible rather than a silent wait.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !staleOnly {
				return fmt.Errorf("name quadlets to restart, or pass --stale to restart every unit running outdated config")
			}
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			rows, pending, err := lifecycleRows(out, cfg, args, staleOnly, true)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Fprintln(out, "Nothing to restart.")
				if staleOnly && pending > 0 {
					fmt.Fprintln(out, yellow(fmt.Sprintf("! %d file(s) have unapplied changes — staleness begins after 'crei apply'", pending)))
				}
				return nil
			}
			// Interactive --stale: pick which stale units to restart. Unlike
			// crei image update (a bigger decision, so it starts unselected),
			// the user already asked to restart stale units, so everything
			// starts selected, deselect to skip any. -y (or no TTY) keeps the
			// list+confirm-all flow.
			if staleOnly && !yes && stdinIsTTY(cmd.InOrStdin()) {
				var chosen []int
				opts := make([]huh.Option[int], len(rows))
				for k, r := range rows {
					opts[k] = huh.NewOption(staleOptionLabel(r), k).Selected(true)
				}
				err := huh.NewForm(huh.NewGroup(
					huh.NewMultiSelect[int]().
						Title("Stale units").
						Description("all selected; space toggles, enter restarts the selection").
						Options(opts...).
						Value(&chosen),
				)).Run()
				if errors.Is(err, huh.ErrUserAborted) {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
				if err != nil {
					return fmt.Errorf("interactive selection unavailable (%v); use -y to restart all stale units", err)
				}
				if len(chosen) == 0 {
					fmt.Fprintln(out, "Nothing selected.")
					return nil
				}
				picked := make([]statusRow, 0, len(chosen))
				for _, k := range chosen {
					picked = append(picked, rows[k])
				}
				rows = picked
			}

			units := serviceNames(rows)
			fmt.Fprintf(out, "Restarting %d unit(s):\n", len(rows))
			// The staleness delta on each line (like status) shows what the
			// restart is for; padding is computed on plain strings so ANSI
			// styling never skews the column.
			width := 0
			for _, r := range rows {
				if len(r.Service) > width {
					width = len(r.Service)
				}
			}
			for _, r := range rows {
				line := "  " + r.Service
				if r.Stale {
					note := "(stale)"
					if r.StaleNote != "" {
						note = "(stale: " + r.StaleNote + ")"
					}
					line += strings.Repeat(" ", width-len(r.Service)+2) + yellow(note)
				}
				fmt.Fprintln(out, line)
			}
			// The picker's selection is the consent; only the non-picker
			// paths (plain restart, -y off with no TTY) still confirm.
			if !yes && (!staleOnly || !stdinIsTTY(cmd.InOrStdin())) {
				ok, err := confirm(cmd.InOrStdin(), out, "Restart?")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
			}
			return trackedRestart(out, underHome(cfg.QuadletDir), units)
		},
	}
	cmd.Flags().BoolVar(&staleOnly, "stale", false, "restart only units whose running process predates the last applied config")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func newLogsCmd() *cobra.Command {
	var staleOnly, follow bool
	var lines int
	cmd := &cobra.Command{
		Use:   "logs [quadlet...]",
		Short: "Show journal logs for quadlet units (journalctl passthrough)",
		Long: "logs runs journalctl for the named quadlets' units (or, with --stale,\n" +
			"for every unit running outdated config), in the scope the quadlet\n" +
			"directory implies. Extra journalctl behavior comes from -f and -n.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !staleOnly {
				return fmt.Errorf("name quadlets, or pass --stale for units running outdated config")
			}
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			rows, _, err := lifecycleRows(out, cfg, args, staleOnly, false)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Fprintln(out, "No units.")
				return nil
			}
			units := serviceNames(rows)
			jargs := []string{}
			if underHome(cfg.QuadletDir) {
				jargs = append(jargs, "--user")
			}
			for _, u := range units {
				jargs = append(jargs, "-u", u)
			}
			if follow {
				jargs = append(jargs, "-f")
			}
			if lines > 0 {
				jargs = append(jargs, "-n", strconv.Itoa(lines))
			}
			j := exec.Command("journalctl", jargs...)
			j.Stdout = out
			j.Stderr = cmd.ErrOrStderr()
			j.Stdin = cmd.InOrStdin()
			return j.Run()
		},
	}
	cmd.Flags().BoolVar(&staleOnly, "stale", false, "show logs only for units whose running process predates the last applied config")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow the journal")
	cmd.Flags().IntVarP(&lines, "lines", "n", 0, "limit to the last N lines per journalctl -n")
	return cmd
}
