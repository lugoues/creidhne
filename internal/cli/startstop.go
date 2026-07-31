package cli

import (
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/reconcile"
)

// daemonReloadFn is indirected so tests can observe the reload without systemd.
var daemonReloadFn = reconcile.DaemonReload

// runnableExts are the unit kinds start and stop target: the leaves that run.
// Infra units stay untouched in both directions. Starting them is redundant
// (the leaves' own Requires=/After= pull them in) and starting a .build
// explicitly would rebuild the image; stopping them is dangerous — quadlet
// wires Requires= from every attaching container to a network's service, so
// stopping a shared network's oneshot propagates the stop to attachers in
// other quadlets (stop the pair network, stop traefik, stop everything behind
// traefik).
var runnableExts = map[string]bool{".container": true, ".pod": true, ".kube": true}

// runnableRows selects what start/stop act on: each quadlet's runnable leaves.
// A quadlet with no runnable unit (pure infrastructure: volumes, networks)
// falls back to all its services — naming an infra-only quadlet is an explicit
// request to act on its infra, cascade included.
func runnableRows(rows []statusRow) []statusRow {
	leavesByQuad := map[string]bool{}
	for _, r := range rows {
		if runnableExts[path.Ext(r.Path)] {
			leavesByQuad[r.Quadlet] = true
		}
	}
	var out []statusRow
	for _, r := range rows {
		if runnableExts[path.Ext(r.Path)] || !leavesByQuad[r.Quadlet] {
			out = append(out, r)
		}
	}
	return out
}

// lifecycleSelection resolves the names-or---all guard shared by start and
// stop: acting on the whole project must be asked for, never implied.
func lifecycleSelection(args []string, all bool, verb string) error {
	switch {
	case all && len(args) > 0:
		return fmt.Errorf("pass quadlet names or --all, not both")
	case !all && len(args) == 0:
		return fmt.Errorf("name quadlets to %s, or pass --all for every quadlet", verb)
	}
	return nil
}

// upRuntimes are the Runtime states in which a unit counts as already up:
// "running" (active+running) and "active" (a oneshot that completed and
// stayed, e.g. RemainAfterExit).
func isUp(runtime string) bool { return runtime == "running" || runtime == "active" }

func newStartCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "start [quadlet...]",
		Short: "Start quadlet units via systemctl (runnable units; deps start on their own)",
		Long: "start starts the named quadlets' runnable units (containers, pods,\n" +
			"kubes) in one ordered systemctl transaction; volumes, networks, and\n" +
			"builds are pulled in by the units' own dependencies, never started\n" +
			"directly (starting a build explicitly would rebuild its image). A\n" +
			"quadlet with only infrastructure units starts those instead.\n\n" +
			"Idempotent: units already up are checked off immediately, and nothing\n" +
			"is enqueued for them. Runtime-only — 'starts at boot' is the\n" +
			"declarative Install: WantedBy in the CUE, which this never touches.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := lifecycleSelection(args, all, "start"); err != nil {
				return err
			}
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			rows, _, err := lifecycleRows(out, cfg, args, false, false)
			if err != nil {
				return err
			}
			rows = runnableRows(rows)
			if len(rows) == 0 {
				fmt.Fprintln(out, "Nothing to start.")
				return nil
			}
			// Out-of-band edits leave systemd loading old definitions; starting
			// those would silently run stale config. Offer the reload (prompt
			// only on a TTY; otherwise warn and continue).
			if err := offerDaemonReload(cmd, out, cfg, rows); err != nil {
				return err
			}
			preDone := map[string]string{}
			for _, r := range rows {
				if isUp(r.Runtime) {
					preDone[r.Service] = "already running"
				}
			}
			if len(preDone) == len(rows) {
				fmt.Fprintf(out, "Nothing to start: all %d unit(s) already running.\n", len(rows))
				return nil
			}
			return trackedTransition(out, cmd.InOrStdin(), rows, underHome(cfg.QuadletDir), false, startSpec, preDone)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "start every quadlet in the project")
	return cmd
}

func newStopCmd() *cobra.Command {
	var all, yes bool
	cmd := &cobra.Command{
		Use:   "stop [quadlet...]",
		Short: "Stop quadlet units via systemctl",
		Long: "stop stops the named quadlets' runnable units (containers, pods,\n" +
			"kubes) in one ordered systemctl transaction. Volume/network/build\n" +
			"units are left alone: quadlet wires Requires= from every attaching\n" +
			"container to a network's service, so stopping a shared network would\n" +
			"propagate the stop to attachers in other quadlets (stop the pair\n" +
			"network -> stop traefik -> stop everything behind traefik). The\n" +
			"created objects outlive their units anyway. Units already down are\n" +
			"checked off immediately.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := lifecycleSelection(args, all, "stop"); err != nil {
				return err
			}
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			rows, _, err := lifecycleRows(out, cfg, args, false, false)
			if err != nil {
				return err
			}
			rows = runnableRows(rows)
			if len(rows) == 0 {
				fmt.Fprintln(out, "Nothing to stop.")
				return nil
			}
			preDone := map[string]string{}
			for _, r := range rows {
				if !isUp(r.Runtime) && r.Runtime != "activating" {
					preDone[r.Service] = "not running"
				}
			}
			if len(preDone) == len(rows) {
				fmt.Fprintf(out, "Nothing to stop: no unit(s) running.\n")
				return nil
			}
			return trackedTransition(out, cmd.InOrStdin(), rows, underHome(cfg.QuadletDir), !yes, stopSpec, preDone)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "stop every quadlet in the project")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

// offerDaemonReload checks the selected rows for units systemd loaded before
// the last file change ("reload needed") and offers a daemon-reload: prompted
// on a TTY, a warning otherwise. Declining (or non-TTY) continues with the old
// definitions, loudly.
func offerDaemonReload(cmd *cobra.Command, out io.Writer, cfg config, rows []statusRow) error {
	var outdated []string
	for _, r := range rows {
		if r.Loaded == "reload needed" {
			outdated = append(outdated, r.Service)
		}
	}
	if len(outdated) == 0 {
		return nil
	}
	if !stdinIsTTY(cmd.InOrStdin()) {
		fmt.Fprintln(out, yellow("! systemd is out of sync with the unit files ("+strings.Join(outdated, ", ")+"); run systemctl daemon-reload"))
		return nil
	}
	ok, err := confirm(cmd.InOrStdin(), out, "systemd is out of sync with the unit files; daemon-reload first?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, yellow("! starting with the definitions systemd has loaded (old)"))
		return nil
	}
	return daemonReloadFn(underHome(cfg.QuadletDir))
}
