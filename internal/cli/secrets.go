package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/podman"
)

// podmanListSecrets is a package var so tests can stub podman, which isn't
// available in the test/CI environment.
var podmanListSecrets = podman.ListSecrets

func newSecretsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "secrets",
		Short: "List the secret registry and whether each secret exists in podman",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			overlay, err := buildOverlay(cfg.ProjectDir)
			if err != nil {
				return err
			}
			declared, err := eval.SecretRegistry(cfg.ProjectDir, overlay, cfg.SecretsField)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(declared) == 0 {
				fmt.Fprintf(out, "No secrets declared in the %q registry.\n", cfg.SecretsField)
				return nil
			}
			existing, err := podmanListSecrets()
			if err != nil {
				return err
			}
			// Only the final (status) column is colored, so its ANSI codes don't
			// disturb tabwriter's width accounting for the name column.
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SECRET\tPODMAN")
			missing := 0
			for _, name := range declared {
				if existing[name] {
					fmt.Fprintf(w, "%s\t%s\n", name, green("present"))
				} else {
					missing++
					fmt.Fprintf(w, "%s\t%s\n", name, red("missing"))
				}
			}
			_ = w.Flush()
			fmt.Fprintf(out, "\n%d secret(s): %d present, %d missing\n", len(declared), len(declared)-missing, missing)
			if missing > 0 {
				fmt.Fprintln(out, dim("Create the missing ones with: crei secrets create -a"))
			}
			return nil
		},
	}
}
