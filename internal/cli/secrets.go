package cli

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/podman"
)

// Overridable in tests: podman isn't available there, and the huh form needs a
// TTY, so the secret value is gathered through a swappable function.
var (
	podmanListSecrets  = podman.ListSecrets
	podmanSecretInfos  = podman.SecretInfos
	podmanCreateSecret = podman.CreateSecret
	podmanRemoveSecret = podman.RemoveSecret
	podmanReadSecret   = podman.ReadSecret
	secretValuer       = promptSecretValue
)

// secretAge humanizes a podman timestamp for the list table.
func secretAge(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return humanDuration(time.Since(t)) + " ago"
}

// printSecretTable renders the list via lipgloss/table, which does its own
// ANSI-aware width accounting (tabwriter counts escape bytes as width).
func printSecretTable(out io.Writer, rows [][]string) {
	t := table.New().
		Border(lipgloss.Border{}).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderHeader(false).BorderRow(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle()
			if col < 4 {
				s = s.PaddingRight(2)
			}
			if row == table.HeaderRow {
				return s.Faint(true)
			}
			cell := rows[row][col]
			switch col {
			case 1:
				if cell == "missing" {
					return s.Inherit(redStyle)
				}
			case 2:
				switch {
				case cell == "yes":
					return s.Inherit(greenStyle)
				case strings.HasPrefix(cell, "no "):
					return s.Faint(true)
				}
			}
			return s
		}).
		Headers("SECRET", "PODMAN", "MANAGED", "CREATED", "UPDATED").
		Rows(rows...)
	for _, line := range strings.Split(t.Render(), "\n") {
		fmt.Fprintln(out, strings.TrimRight(line, " "))
	}
}

func newSecretsCmd() *cobra.Command {
	// A command group: bare `crei secrets` prints help (like `podman secret`),
	// and an unknown subcommand errors rather than silently succeeding.
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Inspect and create podman secrets from the registry",
		Long: "secrets works with the #SecretRegistry declared in your CUE (the\n" +
			"top-level \"secrets\" field by default). 'list' shows which registry\n" +
			"secrets exist in podman; 'create' adds the missing ones; 'prune'\n" +
			"deletes crei-created secrets nothing references anymore; 'adopt'\n" +
			"labels pre-existing registry secrets as crei-managed.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
		},
	}
	cmd.AddCommand(newSecretsListCmd(), newSecretsCreateCmd(), newSecretsPruneCmd(), newSecretsAdoptCmd())
	return cmd
}

// referencedSecretNames is every secret name the project claims: the registry
// plus every Secret= entry on rendered container and build units (a raw
// string reference never registered still counts). Secrets inside .kube YAML
// are not parsed; declare those in the registry to protect them.
func referencedSecretNames(cfg config) (map[string]bool, error) {
	overlay, err := buildOverlay(cfg.ProjectDir)
	if err != nil {
		return nil, err
	}
	declared, err := eval.SecretRegistry(cfg.ProjectDir, overlay, cfg.SecretsField)
	if err != nil {
		return nil, err
	}
	referenced := map[string]bool{}
	for _, n := range declared {
		referenced[n] = true
	}
	quads, err := loadQuadlets(cfg.ProjectDir)
	if err != nil {
		return nil, err
	}
	addEntry := func(v any) {
		if s, ok := v.(string); ok && s != "" {
			name, _, _ := strings.Cut(s, ",")
			referenced[name] = true
		}
	}
	for _, q := range quads {
		for _, u := range q.Units {
			if entries, ok := u.Data["secretStrings"].([]any); ok {
				for _, e := range entries {
					addEntry(e)
				}
			}
			if build, ok := u.Data["Build"].(map[string]any); ok {
				if entries, ok := build["Secret"].([]any); ok {
					for _, e := range entries {
						addEntry(e)
					}
				}
			}
		}
	}
	return referenced, nil
}

func newSecretsPruneCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete crei-created secrets that nothing references anymore",
		Long: "prune deletes podman secrets that carry the crei label\n" +
			"(" + podman.ManagedLabel + ") and are no longer referenced: not in the\n" +
			"registry and not a Secret= entry on any unit. Secrets without the\n" +
			"label were not created by crei and are never touched (adopt labels\n" +
			"pre-existing ones). Secrets referenced only inside .kube YAML are\n" +
			"invisible here; declare them in the registry.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			referenced, err := referencedSecretNames(cfg)
			if err != nil {
				return err
			}
			infos, err := podmanSecretInfos()
			if err != nil {
				return err
			}

			var doomed, unmanaged []string
			kept := 0
			for _, name := range sortedKeys(infos) {
				switch {
				case referenced[name]:
					kept++
				case infos[name].Managed:
					doomed = append(doomed, name)
				default:
					unmanaged = append(unmanaged, name)
				}
			}

			if len(doomed) == 0 {
				fmt.Fprintf(out, "Nothing to prune: %d referenced, %d unmanaged (not created by crei).\n", kept, len(unmanaged))
				return nil
			}
			for _, name := range doomed {
				fmt.Fprintf(out, "%s %s\n", red("delete"), name)
			}
			fmt.Fprintf(out, "\n%d to delete, %d kept (referenced), %d skipped (not created by crei)\n", len(doomed), kept, len(unmanaged))
			if !yes {
				ok, err := confirm(cmd.InOrStdin(), out, fmt.Sprintf("Delete %d secret(s)?", len(doomed)))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
			}
			failed := 0
			for _, name := range doomed {
				if err := podmanRemoveSecret(name); err != nil {
					failed++
					fmt.Fprintln(out, red("  "+err.Error()))
					continue
				}
				fmt.Fprintf(out, "%s deleted\n", name)
			}
			if failed > 0 {
				return fmt.Errorf("%d secret(s) could not be deleted (in use by a container?)", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "delete without confirming")
	return cmd
}

func newSecretsAdoptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "adopt",
		Short: "Label pre-existing registry secrets as crei-managed",
		Long: "adopt re-creates registry-declared secrets that exist in podman\n" +
			"without the crei label, preserving their value byte-exact (read via\n" +
			"inspect --showsecret, piped back to create --replace). Only registry\n" +
			"names are candidates: another tool's secrets cannot be claimed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			overlay, err := buildOverlay(cfg.ProjectDir)
			if err != nil {
				return err
			}
			declared, err := eval.SecretRegistry(cfg.ProjectDir, overlay, cfg.SecretsField)
			if err != nil {
				return err
			}
			infos, err := podmanSecretInfos()
			if err != nil {
				return err
			}
			adopted := 0
			for _, name := range declared {
				info, exists := infos[name]
				if !exists || info.Managed {
					continue
				}
				value, err := podmanReadSecret(name)
				if err != nil {
					return err
				}
				if err := podmanCreateSecret(name, value, true); err != nil {
					return err
				}
				adopted++
				fmt.Fprintf(out, "%s adopted\n", green(name))
			}
			if adopted == 0 {
				fmt.Fprintln(out, "Nothing to adopt: every existing registry secret is already managed.")
			}
			return nil
		},
	}
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
			infos, err := podmanSecretInfos()
			if err != nil {
				return err
			}
			var rows [][]string
			missing := 0
			for _, name := range declared {
				info, exists := infos[name]
				switch {
				case exists && info.Managed:
					rows = append(rows, []string{name, "present", "yes", secretAge(info.CreatedAt), secretAge(info.UpdatedAt)})
				case exists:
					rows = append(rows, []string{name, "present", "no (crei secrets adopt)", secretAge(info.CreatedAt), secretAge(info.UpdatedAt)})
				default:
					missing++
					rows = append(rows, []string{name, "missing", "-", "-", "-"})
				}
			}
			printSecretTable(out, rows)
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
