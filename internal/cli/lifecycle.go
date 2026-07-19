package cli

import (
	"fmt"
	"io"
	"os/exec"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/systemd"
)

// lifecycleUnits selects the services acted on by restart/logs: every unit of
// the named quadlets, narrowed to stale ones by staleOnly. skipUnrestartable
// drops (with a warning) stale units whose change a restart cannot apply.
func lifecycleUnits(out io.Writer, cfg config, args []string, staleOnly, skipUnrestartable bool) ([]string, error) {
	in, notes, err := gatherStatus(cfg, args)
	if err != nil {
		return nil, err
	}
	for _, n := range notes {
		fmt.Fprintln(out, yellow("! "+n))
	}
	var units []string
	for _, r := range classifyRows(in) {
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
		units = append(units, r.Service)
	}
	return units, nil
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
			"a warning; see 'crei diff --stale' for what each restart would change.",
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
			units, err := lifecycleUnits(out, cfg, args, staleOnly, true)
			if err != nil {
				return err
			}
			if len(units) == 0 {
				fmt.Fprintln(out, "Nothing to restart.")
				return nil
			}
			fmt.Fprintf(out, "Restarting %d unit(s):\n", len(units))
			for _, u := range units {
				fmt.Fprintln(out, "  "+u)
			}
			if !yes {
				ok, err := confirm(cmd.InOrStdin(), out, "Restart?")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
			}
			if err := systemd.Restart(underHome(cfg.QuadletDir), units); err != nil {
				return err
			}
			fmt.Fprintln(out, "Restarted.")
			return nil
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
			units, err := lifecycleUnits(out, cfg, args, staleOnly, false)
			if err != nil {
				return err
			}
			if len(units) == 0 {
				fmt.Fprintln(out, "No units.")
				return nil
			}
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
