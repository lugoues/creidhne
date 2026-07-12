package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/importer"
)

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Convert existing definitions into creidhne CUE",
	}
	cmd.AddCommand(newImportComposeCmd())
	return cmd
}

func newImportComposeCmd() *cobra.Command {
	var (
		output   string
		name     string
		envFiles []string
		useOsEnv bool
		resolve  bool
		force    bool
		pkg      string
		preserve bool
		embed    bool
	)
	cmd := &cobra.Command{
		Use:   "compose [compose-file|url...]",
		Short: "Convert a docker-compose project into a creidhne CUE file",
		Long: "import compose converts a compose project into one #Quadlet: services\n" +
			"become containers, named volumes/networks become units referenced via\n" +
			"#self, build sections become build units, and compose secrets map onto\n" +
			"the secret registry (values are never imported; the report tells you how\n" +
			"to load them).\n\n" +
			"With no arguments the compose file is discovered like docker compose\n" +
			"does (compose.yaml and friends, plus override files). Arguments may be\n" +
			"http(s) URLs: GitHub/GitLab file links are fetched (browser blob URLs\n" +
			"are rewritten to their raw form) and the project name derives from the\n" +
			"URL directory unless the file sets name: or --name is given. Relative\n" +
			"paths in a fetched file (build contexts, bind mounts) refer to the\n" +
			"source repository layout.\n\n" +
			"By default ${VAR} references are not resolved: they are lifted into an\n" +
			"env: struct that validate forces you to fill. Pass --env-file/--env to\n" +
			"resolve and bake the values at import time instead.\n\n" +
			"Anything that cannot be represented is listed in the conversion report,\n" +
			"never dropped silently.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			res, err := importer.Convert(importer.Options{
				Paths:         args,
				WorkingDir:    wd,
				ProjectName:   name,
				ResolveEnv:    resolve || len(envFiles) > 0 || useOsEnv,
				EnvFiles:      envFiles,
				UseOsEnv:      useOsEnv,
				Package:       pkg,
				PreserveNames: preserve,
				OmitSource:    !embed,
			})
			if err != nil {
				return err
			}

			dest := output
			if dest == "" {
				dest = res.QuadletName + ".cue"
			}
			if dest == "-" {
				fmt.Fprint(out, string(res.CUE))
			} else {
				if _, err := os.Stat(dest); err == nil && !force {
					return fmt.Errorf("%s already exists (use --force to overwrite)", dest)
				} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
					return err
				}
				if err := os.WriteFile(dest, res.CUE, 0o644); err != nil {
					return err
				}
				abs, _ := filepath.Abs(dest)
				fmt.Fprintf(out, "wrote %s\n", abs)
			}

			if len(res.Notes) > 0 {
				fmt.Fprintln(out)
				for _, n := range res.Notes {
					fmt.Fprintln(out, dim("note: "+n))
				}
			}
			if len(res.Warnings) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, yellow(fmt.Sprintf("%d warning(s):", len(res.Warnings))))
				for _, w := range res.Warnings {
					fmt.Fprintln(out, "  ! "+w)
				}
			}
			if len(res.Steps) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "next steps:")
				for _, s := range res.Steps {
					fmt.Fprintln(out, "  - "+s)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default <project>.cue; - for stdout)")
	cmd.Flags().StringVar(&name, "name", "", "override the project (quadlet) name")
	cmd.Flags().StringArrayVar(&envFiles, "env-file", nil, "resolve ${VAR} from this env file at import time (repeatable)")
	cmd.Flags().BoolVar(&useOsEnv, "env", false, "resolve ${VAR} from the process environment at import time")
	cmd.Flags().BoolVar(&resolve, "resolve", false, "resolve ${VAR} using only the file's own defaults (plus .env if present)")
	cmd.Flags().StringVar(&pkg, "package", "quadlets", "CUE package name for the emitted file (must match other .cue files in the project)")
	cmd.Flags().BoolVar(&embed, "embed-source", true, "embed the source compose file(s) as a trailing comment block for reference")
	cmd.Flags().BoolVar(&preserve, "preserve-names", false, "adopt the compose-era volume/network runtime names so an existing deployment's data is reused (migration; externals are always adopted by name)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite the output file if it exists")
	return cmd
}
