package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
)

func newSecretRemoveCmd() *cobra.Command {
	var del, yes bool
	cmd := &cobra.Command{
		Use:     "remove <name...>",
		Aliases: []string{"rm"},
		Short:   "Unregister a secret from the crei-owned registry",
		Long: "remove drops entries from registries/secrets.cue. By default it only\n" +
			"unregisters (the podman secret is left alone); pass --delete to also\n" +
			"remove the value from podman. Only crei-owned registry entries can be\n" +
			"removed; a secret declared solely in the hand-authored top-level field\n" +
			"lives in a file crei does not own, so edit that file directly.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, projectDir, err := loadSecretRegistry()
			if err != nil {
				return err
			}
			out := styledOut(cmd.OutOrStdout())
			if len(entries) == 0 {
				fmt.Fprintln(out, "No crei-owned secret registry (registries/secrets.cue).")
				return nil
			}

			// Resolve every name first so a typo aborts before anything is
			// written or deleted.
			var idxs []int
			for _, name := range args {
				i, err := findSecret(entries, name)
				if err != nil {
					return err
				}
				idxs = append(idxs, i)
			}

			// --delete is destructive against podman, so confirm the podman
			// removals (the registry edit itself is a reviewable diff).
			if del && !yes {
				ok, err := confirm(cmd.InOrStdin(), out,
					fmt.Sprintf("Also delete %d secret(s) from podman?", len(idxs)))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
			}

			var podmanFailed int
			kept := make([]eval.SecretEntry, 0, len(entries))
			drop := map[int]bool{}
			for _, i := range idxs {
				drop[i] = true
			}
			for i, e := range entries {
				if !drop[i] {
					kept = append(kept, e)
					continue
				}
				if del {
					// Best-effort: a secret in use by a container cannot be
					// removed; report but still unregister it.
					if err := podmanRemoveSecret(e.Name); err != nil {
						podmanFailed++
						fmt.Fprintln(out, red("  "+err.Error()))
					} else {
						fmt.Fprintf(out, "%s %s deleted from podman\n", red("-"), e.Name)
					}
				}
				fmt.Fprintf(out, "%s %s unregistered\n", green("~"), e.Key)
			}

			if err := writeSecretRegistry(projectDir, kept); err != nil {
				return err
			}
			fmt.Fprintf(out, "\nRemoved %d entr(y/ies) from registries/secrets.cue.\n", len(idxs))
			if podmanFailed > 0 {
				return fmt.Errorf("%d secret(s) could not be deleted from podman (in use by a container?)", podmanFailed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&del, "delete", false, "also delete the value from podman")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the --delete confirmation")
	return cmd
}
