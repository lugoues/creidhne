package cli

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/podman"
)

// Overridable in tests: podman isn't available there, and the huh form needs a
// TTY, so the secret value is gathered through a swappable function.
var (
	podmanListSecrets  = podman.ListSecrets
	podmanCreateSecret = podman.CreateSecret
	secretValuer       = promptSecretValue
)

func newSecretsCmd() *cobra.Command {
	// A command group: bare `crei secrets` prints help (like `podman secret`),
	// and an unknown subcommand errors rather than silently succeeding.
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Inspect and create podman secrets from the registry",
		Long: "secrets works with the #SecretRegistry declared in your CUE (the\n" +
			"top-level \"secrets\" field by default). 'list' shows which registry\n" +
			"secrets exist in podman; 'create' adds the missing ones.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
		},
	}
	cmd.AddCommand(newSecretsListCmd(), newSecretsCreateCmd())
	return cmd
}

func newSecretsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the secret registry and whether each secret exists in podman",
		Args:    cobra.NoArgs,
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

func newSecretsCreateCmd() *cobra.Command {
	var all, replace bool
	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a podman secret (interactively), entering or generating its value",
		Long: "create makes a podman secret. Pass a name to create one, or -a to walk\n" +
			"through every registry secret missing from podman. For each, you can type a\n" +
			"value (hidden) or generate a random one.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all == (len(args) == 1) {
				return fmt.Errorf("specify exactly one of: a secret name, or -a/--all")
			}
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			existing, err := podmanListSecrets()
			if err != nil {
				return err
			}

			var targets []string
			if all {
				overlay, err := buildOverlay(cfg.ProjectDir)
				if err != nil {
					return err
				}
				declared, err := eval.SecretRegistry(cfg.ProjectDir, overlay, cfg.SecretsField)
				if err != nil {
					return err
				}
				for _, n := range declared {
					if !existing[n] {
						targets = append(targets, n)
					}
				}
				if len(targets) == 0 {
					fmt.Fprintln(out, "All registry secrets already exist in podman.")
					return nil
				}
			} else {
				targets = []string{args[0]}
			}

			for _, name := range targets {
				if existing[name] && !replace {
					fmt.Fprintf(out, "%s already exists, skipping (use --replace to overwrite)\n", name)
					continue
				}
				value, generated, err := secretValuer(name)
				if err != nil {
					return err
				}
				if err := podmanCreateSecret(name, value, replace); err != nil {
					return err
				}
				fmt.Fprintf(out, "%s created\n", green(name))
				if generated {
					fmt.Fprintln(out, dim("  save this value now; it will not be shown again:"))
					fmt.Fprintf(out, "  %s\n", string(value))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "walk through every registry secret missing from podman")
	cmd.Flags().BoolVar(&replace, "replace", false, "overwrite a secret that already exists")
	return cmd
}

// promptSecretValue asks (via huh, so it needs a TTY) whether to enter a value
// or generate one, and returns the value plus whether it was generated.
func promptSecretValue(name string) ([]byte, bool, error) {
	mode := "enter"
	typed := ""
	length := "32"
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Secret %q", name)).
				Options(
					huh.NewOption("Enter a value", "enter"),
					huh.NewOption("Generate a random one", "generate"),
				).
				Value(&mode),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Value").
				EchoMode(huh.EchoModePassword).
				Value(&typed).
				Validate(func(s string) error {
					if s == "" {
						return errors.New("value cannot be empty")
					}
					return nil
				}),
		).WithHideFunc(func() bool { return mode != "enter" }),
		huh.NewGroup(
			huh.NewInput().
				Title("Length").
				Value(&length).
				Validate(validateLength),
		).WithHideFunc(func() bool { return mode != "generate" }),
	)
	if err := form.Run(); err != nil {
		return nil, false, err
	}
	if mode == "generate" {
		n, _ := strconv.Atoi(strings.TrimSpace(length)) // already validated by the form
		v, err := generatePassword(n)
		return v, true, err
	}
	return []byte(typed), false, nil
}

func validateLength(s string) error {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err != nil || n < 1 {
		return errors.New("length must be a positive integer")
	}
	return nil
}

const passwordCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// generatePassword returns n cryptographically-random alphanumeric bytes (no
// symbols, to avoid escaping pitfalls in downstream consumers). n <= 0 yields 32.
func generatePassword(n int) ([]byte, error) {
	if n <= 0 {
		n = 32
	}
	limit := big.NewInt(int64(len(passwordCharset)))
	b := make([]byte, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return nil, err
		}
		b[i] = passwordCharset[idx.Int64()]
	}
	return b, nil
}
