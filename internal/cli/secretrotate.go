package cli

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
)

// generateFromPolicy synthesizes a secret value per a generate policy: length
// characters from the chosen alphabet. Charset "" or "alphanumeric" reuses the
// symbol-free password generator; "hex" and "base64" (url-safe) trim a random
// encoding to length. All draw from crypto/rand.
func generateFromPolicy(g *eval.SecretGenerate) ([]byte, error) {
	n := g.Length
	if n <= 0 {
		n = defaultSecretLength
	}
	switch g.Charset {
	case "", "alphanumeric":
		return generatePassword(n)
	case "hex":
		raw := make([]byte, (n+1)/2)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		return []byte(hex.EncodeToString(raw))[:n], nil
	case "base64":
		raw := make([]byte, (n*3+3)/4)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		return []byte(base64.RawURLEncoding.EncodeToString(raw))[:n], nil
	default:
		return nil, fmt.Errorf("unknown charset %q", g.Charset)
	}
}

func newSecretRotateCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "rotate [name...]",
		Short: "Regenerate crei-owned secrets and replace them in podman",
		Long: "rotate makes a fresh value for each named secret (or every crei-owned\n" +
			"secret with a generate policy, when no names are given) and writes it\n" +
			"into podman, replacing the old value. Only entries in\n" +
			"registries/secrets.cue with a generate policy can rotate; a manual\n" +
			"entry has no way to synthesize a value ('crei secret create <name>\n" +
			"--replace' to set one by hand).\n\n" +
			"Rotating replaces a live value: a container already running reads its\n" +
			"secret at start, so restart the consumers afterwards to pick up the\n" +
			"new value.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, _, err := loadSecretRegistry()
			if err != nil {
				return err
			}
			out := styledOut(cmd.OutOrStdout())
			if len(entries) == 0 {
				fmt.Fprintln(out, "No crei-owned secret registry (registries/secrets.cue).")
				return nil
			}

			// Resolve the target set. Named entries must exist (a typo aborts);
			// with no names, every generate-policy entry is a candidate.
			var targets []eval.SecretEntry
			if len(args) > 0 {
				for _, name := range args {
					i, err := findSecret(entries, name)
					if err != nil {
						return err
					}
					targets = append(targets, entries[i])
				}
			} else {
				targets = entries
			}

			// Split into rotatable (has a generate policy) and skipped (manual).
			// A manual entry named explicitly is a user error worth surfacing;
			// one merely swept up by the no-args case is just skipped.
			var rotatable, manual []eval.SecretEntry
			for _, e := range targets {
				if e.Generate != nil {
					rotatable = append(rotatable, e)
				} else {
					manual = append(manual, e)
				}
			}
			for _, e := range manual {
				fmt.Fprintf(out, "  %s %s (manual; use 'crei secret create %s --replace')\n", dim("-"), e.Key, e.Key)
			}
			if len(rotatable) == 0 {
				fmt.Fprintln(out, "Nothing to rotate.")
				return nil
			}

			// Rotating overwrites live values, so confirm unless -y.
			if !yes {
				for _, e := range rotatable {
					fmt.Fprintf(out, "%s %s\n", yellow("rotate"), e.Name)
				}
				ok, err := confirm(cmd.InOrStdin(), out, fmt.Sprintf("Replace %d secret value(s) in podman?", len(rotatable)))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
			}

			rotated := 0
			for _, e := range rotatable {
				value, err := generateFromPolicy(e.Generate)
				if err != nil {
					return fmt.Errorf("%s: %w", e.Key, err)
				}
				if err := podmanCreateSecret(e.Name, value, true); err != nil {
					return fmt.Errorf("%s: %w", e.Name, err)
				}
				rotated++
				fmt.Fprintf(out, "%s %s rotated\n", green("~"), e.Name)
			}
			fmt.Fprintf(out, "\nRotated %d secret(s). Restart their consumers to pick up the new values.\n", rotated)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "rotate without confirming")
	return cmd
}
