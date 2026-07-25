package cli

import (
	"bufio"
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

// spinFrames is the braille cycle huh's spinner uses; reused so restart's
// per-unit spinner matches the rest of the CLI.
var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinInterval is the animation tick; a poll interval spans several frames.
// Both are package vars so tests drive the loop without real time.
var spinInterval = 100 * time.Millisecond

// restartLive reports whether out is an interactive terminal, so restart can
// redraw its unit block in place. A package var so tests exercise both paths.
var restartLive = func(out io.Writer) bool {
	f, ok := out.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// restartTracker renders a restart's progress: each unit's line carries a
// leading glyph, spinner while its job is pending, check when it clears, cross
// if it came back failed.
type restartTracker struct {
	out       io.Writer
	rows      []statusRow
	units     []string
	userScope bool
	width     int             // widest service name, for note alignment
	done      map[string]bool // units whose job has left the queue
}

// trackedRestart lists the units, optionally confirms, then enqueues a
// non-blocking restart (--no-block, so systemd still orders the jobs by their
// dependencies) and tracks completion. On a terminal it prints the unit block
// once and updates those same lines in place — a spinner while a job is
// pending, a check when it clears, a cross if it came back failed — with the
// confirm prompt sitting below the block and erased before the animation, so
// there is a single list. Off a terminal each unit prints once as it completes.
// needConfirm asks before restarting; a failed unit turns the command non-zero.
func trackedRestart(out io.Writer, in io.Reader, rows []statusRow, userScope bool, needConfirm bool) error {
	units := serviceNames(rows)
	if len(units) == 0 {
		return nil
	}
	t := &restartTracker{out: out, rows: rows, units: units, userScope: userScope, done: map[string]bool{}}
	t.width = maxServiceWidth(rows)
	if restartLive(out) {
		return t.liveFlow(in, needConfirm)
	}
	return t.plainFlow(in, needConfirm)
}

// readYesNo prints prompt and reads a y/N answer from in. Inline (no huh) so the
// caller keeps full control of the terminal, which the in-place block needs.
func readYesNo(in io.Reader, out io.Writer, prompt string) (bool, error) {
	fmt.Fprint(out, prompt)
	sc := bufio.NewScanner(in)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return false, err
		}
		return false, fmt.Errorf("no confirmation read from stdin; re-run with -y to restart non-interactively")
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes", nil
}

// poll refreshes the done set: a unit absent from the job queue has finished.
func (t *restartTracker) poll() error {
	pending, err := pendingJobsFn(t.userScope, t.units)
	if err != nil {
		return err
	}
	for _, u := range t.units {
		if _, still := pending[u]; !still {
			t.done[u] = true
		}
	}
	return nil
}

func (t *restartTracker) allDone() bool {
	for _, u := range t.units {
		if !t.done[u] {
			return false
		}
	}
	return true
}

// label renders a unit's line body (after the glyph).
func (t *restartTracker) label(r statusRow) string {
	return restartLabel(r, t.width)
}

// maxServiceWidth is the widest service name, for aligning the staleness notes.
func maxServiceWidth(rows []statusRow) int {
	w := 0
	for _, r := range rows {
		if len(r.Service) > w {
			w = len(r.Service)
		}
	}
	return w
}

// restartLabel renders a unit line body: the service name plus its staleness
// note, padded to width so notes align. Padding is measured on the plain name
// so ANSI styling never skews the column.
func restartLabel(r statusRow, width int) string {
	s := r.Service
	if r.Stale {
		note := "stale"
		if r.StaleNote != "" {
			note = "stale: " + r.StaleNote
		}
		s += strings.Repeat(" ", width-len(r.Service)+2) + yellow("("+note+")")
	}
	return s
}

// printRestartPreview lists the units a restart will affect, so the confirm
// prompt isn't blind. A distinct header from the live block's "Restarting …"
// keeps it reading as plan -> confirm -> progress, not a repeated line.
func printRestartPreview(out io.Writer, rows []statusRow) {
	width := maxServiceWidth(rows)
	fmt.Fprintf(out, "%d unit(s) will restart:\n", len(rows))
	for _, r := range rows {
		fmt.Fprintf(out, "  %s\n", restartLabel(r, width))
	}
}

// failures returns the units that cleared the queue but ended failed; a cleared
// job is not proof of success.
func (t *restartTracker) failures() map[string]bool {
	failed := map[string]bool{}
	if statuses, err := restartStatusFn(t.userScope, t.units); err == nil {
		for _, u := range t.units {
			if st, ok := statuses[u]; ok && st.ActiveState == "failed" {
				failed[u] = true
			}
		}
	}
	return failed
}

// failedError names the failed units for a non-zero exit, or nil if none.
func (t *restartTracker) failedError(failed map[string]bool) error {
	var names []string
	for _, u := range t.units {
		if failed[u] {
			names = append(names, u)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return fmt.Errorf("%d unit(s) failed to restart: %s", len(names), strings.Join(names, ", "))
}

// glyph is a unit's leading status marker: cross if failed, check if done, else
// the current spinner frame.
func (t *restartTracker) glyph(svc string, frame int, failed map[string]bool) string {
	switch {
	case failed[svc]:
		return red("✗")
	case t.done[svc]:
		return green("✓")
	default:
		return yellow(spinFrames[frame%len(spinFrames)])
	}
}

// liveFlow (terminal): print the header and unit block once, prompt below it
// when needed (erasing the prompt so the block stays the bottom-most content),
// then enqueue and animate those same lines in place.
func (t *restartTracker) liveFlow(in io.Reader, needConfirm bool) error {
	fmt.Fprintf(t.out, "Restarting %d unit(s):\n", len(t.rows))
	for _, r := range t.rows {
		fmt.Fprintf(t.out, "  %s %s\n", dim("·"), t.label(r)) // pending
	}
	if needConfirm {
		// Save the cursor (just below the block), prompt, then restore and
		// clear to end of screen — wiping the prompt and its echo regardless of
		// how many lines they took, so the block is bottom-most for animating.
		fmt.Fprint(t.out, "\033[s")
		ok, err := readYesNo(in, t.out, "Restart? [y/N] ")
		if err != nil {
			return err
		}
		fmt.Fprint(t.out, "\033[u\033[J")
		if !ok {
			fmt.Fprintln(t.out, "Aborted.")
			return nil
		}
	}
	if err := restartAsyncFn(t.userScope, t.units); err != nil {
		return err
	}
	return t.animate()
}

// animate redraws the already-printed unit block in place each frame: pending
// units show a spinner, completed ones a check. Polls the job queue every
// pollEvery frames so a slow unit reads as working, not hung.
func (t *restartTracker) animate() error {
	pollEvery := int(restartPoll / spinInterval)
	if pollEvery < 1 {
		pollEvery = 1
	}
	for frame := 0; ; frame++ {
		if frame%pollEvery == 0 {
			if err := t.poll(); err != nil {
				return err
			}
		}
		fmt.Fprintf(t.out, "\033[%dA", len(t.rows)) // cursor to the block top
		for _, r := range t.rows {
			fmt.Fprintf(t.out, "\r\033[K  %s %s\n", t.glyph(r.Service, frame, nil), t.label(r))
		}
		if t.allDone() {
			break
		}
		restartSleep(spinInterval)
	}
	// Final pass: overlay failures onto the settled block.
	failed := t.failures()
	if len(failed) > 0 {
		fmt.Fprintf(t.out, "\033[%dA", len(t.rows))
		for _, r := range t.rows {
			fmt.Fprintf(t.out, "\r\033[K  %s %s\n", t.glyph(r.Service, 0, failed), t.label(r))
		}
	}
	return t.failedError(failed)
}

// plainFlow (pipe, CI): no cursor control, so list the units for the confirm,
// then print each once as its job clears in the order they finish.
func (t *restartTracker) plainFlow(in io.Reader, needConfirm bool) error {
	if needConfirm {
		printRestartPreview(t.out, t.rows)
		ok, err := readYesNo(in, t.out, "Restart? [y/N] ")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(t.out, "Aborted.")
			return nil
		}
	}
	fmt.Fprintf(t.out, "Restarting %d unit(s):\n", len(t.rows))
	if err := restartAsyncFn(t.userScope, t.units); err != nil {
		return err
	}
	printed := map[string]bool{}
	emit := func() {
		for _, r := range t.rows {
			if t.done[r.Service] && !printed[r.Service] {
				printed[r.Service] = true
				fmt.Fprintf(t.out, "  %s %s\n", green("✓"), t.label(r))
			}
		}
	}
	for {
		if err := t.poll(); err != nil {
			return err
		}
		emit()
		if t.allDone() {
			break
		}
		restartSleep(restartPoll)
	}
	failed := t.failures()
	for _, r := range t.rows {
		if failed[r.Service] {
			fmt.Fprintf(t.out, "  %s %s\n", red("✗"), r.Service)
		}
	}
	return t.failedError(failed)
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

			// The picker's selection is the consent (and already shows the
			// units); only the non-picker paths (plain restart, -y off with no
			// TTY) still confirm. trackedRestart lists the units and prompts, so
			// the confirm isn't blind.
			needConfirm := !yes && (!staleOnly || !stdinIsTTY(cmd.InOrStdin()))
			return trackedRestart(out, cmd.InOrStdin(), rows, underHome(cfg.QuadletDir), needConfirm)
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
