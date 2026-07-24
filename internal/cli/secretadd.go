package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
)

// defaultSecretLength is the generated-value length when --length is unset,
// matching the interactive create prompt's default.
const defaultSecretLength = 32

func newSecretAddCmd() *cobra.Command {
	var podmanName, charset string
	var length int
	var manual, force bool
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Register a secret in the crei-owned registry (registries/secrets.cue)",
		Long: "add registers a secret in registries/secrets.cue, the crei-owned\n" +
			"registry, recording how 'crei secret create'/'rotate' should generate\n" +
			"its value. It writes no secret material and creates nothing in podman:\n" +
			"run 'crei secret create <name>' to make the value.\n\n" +
			"By default the entry gets a generate policy (length, charset); pass\n" +
			"--manual for a hand-entered secret (a TLS cert, an API token) that crei\n" +
			"should track but never synthesize. --name overrides the podman secret\n" +
			"name, which otherwise equals <name>.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if !cueIdent.MatchString(key) {
				return fmt.Errorf("%q is not a valid entry name (letters, digits, _; no leading digit)", key)
			}
			switch charset {
			case "", "alphanumeric", "hex", "base64":
			default:
				return fmt.Errorf("invalid --charset %q (want alphanumeric, hex, or base64)", charset)
			}
			if manual && (length != 0 || charset != "") {
				return fmt.Errorf("--manual takes no --length/--charset (a manual secret has no generate policy)")
			}

			entries, projectDir, err := loadSecretRegistry()
			if err != nil {
				return err
			}
			for _, e := range entries {
				if e.Key == key && !force {
					return fmt.Errorf("%q already in the registry; use --force to replace", key)
				}
			}

			e := eval.SecretEntry{Key: key, Name: key}
			if podmanName != "" {
				e.Name = podmanName
			}
			if !manual {
				g := &eval.SecretGenerate{Length: defaultSecretLength}
				if length != 0 {
					g.Length = length
				}
				if charset != "" {
					g.Charset = charset
				}
				e.Generate = g
			}

			entries = upsertSecret(entries, e)
			if err := writeSecretRegistry(projectDir, entries); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			how := "manual (value entered at create time)"
			if e.Generate != nil {
				how = fmt.Sprintf("generate %d chars", e.Generate.Length)
				if e.Generate.Charset != "" {
					how += " " + e.Generate.Charset
				}
			}
			fmt.Fprintf(out, "%s %s (%s)\n", green("+"), key, how)
			fmt.Fprintf(out, "  run 'crei secret create %s' to create it in podman\n", key)
			return nil
		},
	}
	cmd.Flags().StringVar(&podmanName, "name", "", "podman secret name (default: the entry name)")
	cmd.Flags().IntVar(&length, "length", 0, "generated value length (default 32)")
	cmd.Flags().StringVar(&charset, "charset", "", "generated value alphabet: alphanumeric (default), hex, base64")
	cmd.Flags().BoolVar(&manual, "manual", false, "register without a generate policy (value entered by hand)")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing entry of the same name")
	return cmd
}

// upsertSecret replaces an entry with the same key or appends a new one.
func upsertSecret(entries []eval.SecretEntry, e eval.SecretEntry) []eval.SecretEntry {
	for i := range entries {
		if entries[i].Key == e.Key {
			entries[i] = e
			return entries
		}
	}
	return append(entries, e)
}
