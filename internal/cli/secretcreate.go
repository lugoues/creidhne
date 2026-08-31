package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
)

// defaultSecretLength is the generated-value length when --length is unset,
// matching the interactive prompt's default.
const defaultSecretLength = 32

// Generated-length bounds shared by --length and the interactive prompt: the
// schema's floor is 8, podman refuses secrets over 512KB, and a multi-KB
// generated password is a typo, not a policy.
const (
	secretLengthMin = 8
	secretLengthMax = 4096
)

// newSecretCreateCmd is the one secret-making verb, podman-shaped: it registers
// the secret in the crei-owned registry (unless already declared) and creates
// its value in podman, in one step. It replaced the former add/create pair.
func newSecretCreateCmd() *cobra.Command {
	var all, replace, manual, force bool
	var podmanName, charset string
	var length int
	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a podman secret, registering it in the crei-owned registry",
		Long: "create makes a podman secret and records it in registries/secrets.cue\n" +
			"in one step. --length/--charset record a generate policy and synthesize\n" +
			"the value with no prompt; --manual records a hand-entered secret and\n" +
			"prompts for its value. With no flags on a terminal you choose at a\n" +
			"prompt, and the choice is what gets recorded (generate -> a policy\n" +
			"rotate can regenerate; enter -> a manual entry).\n\n" +
			"An already-registered name reuses its recorded policy (--force changes\n" +
			"it); a name declared in the hand-authored top-level secrets field is\n" +
			"not re-registered. A secret already in podman is skipped unless\n" +
			"--replace. -a walks every declared secret missing from podman.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all == (len(args) == 1) {
				return fmt.Errorf("specify exactly one of: a secret name, or -a/--all")
			}
			switch charset {
			case "", "alphanumeric", "hex", "base64":
			default:
				return fmt.Errorf("invalid --charset %q (want alphanumeric, hex, or base64)", charset)
			}
			// Validating here keeps an invalid length out of both podman and
			// the registry (a recorded length below the schema floor would
			// fail every later project load).
			if length != 0 && (length < secretLengthMin || length > secretLengthMax) {
				return fmt.Errorf("--length %d is out of range (%d..%d, the schema minimum is %d)", length, secretLengthMin, secretLengthMax, secretLengthMin)
			}
			if manual && (length != 0 || charset != "") {
				return fmt.Errorf("--manual takes no --length/--charset (a manual secret has no generate policy)")
			}
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			out := styledOut(cmd.OutOrStdout())
			existing, err := podmanListSecrets()
			if err != nil {
				return err
			}
			if all {
				return createAllMissing(out, cfg, existing, replace)
			}
			return createOne(out, cfg, createOpts{
				key: args[0], podmanName: podmanName, length: length, charset: charset,
				manual: manual, force: force, replace: replace, existing: existing,
			})
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "walk through every declared secret missing from podman")
	cmd.Flags().BoolVar(&replace, "replace", false, "overwrite a secret that already exists in podman")
	cmd.Flags().StringVar(&podmanName, "name", "", "podman secret name (default: the entry name)")
	cmd.Flags().IntVar(&length, "length", 0, "record a generate policy with this length and synthesize the value")
	cmd.Flags().StringVar(&charset, "charset", "", "generate-policy alphabet: alphanumeric (default), hex, base64")
	cmd.Flags().BoolVar(&manual, "manual", false, "record a manual entry (no generate policy); value is prompted")
	cmd.Flags().BoolVar(&force, "force", false, "change an already-registered entry's policy or podman name")
	return cmd
}

type createOpts struct {
	key, podmanName, charset string
	length                   int
	manual, force, replace   bool
	existing                 map[string]bool
}

// createOne registers (when needed) and creates a single secret. The argument
// is an entry key or a podman secret name; any string works — non-identifier
// registry keys are quoted in the emitted CUE.
func createOne(out io.Writer, cfg config, o createOpts) error {
	entries, projectDir, err := loadSecretRegistry()
	if err != nil {
		return err
	}
	overlay, err := buildOverlay(cfg.ProjectDir)
	if err != nil {
		return err
	}
	handPairs, err := eval.SecretRegistryPairs(cfg.ProjectDir, overlay, cfg.SecretsField)
	if err != nil {
		return err
	}
	// A hand-authored declaration matches by CUE key or by podman name, and
	// matching by key resolves the declared podman name: `create tls_cert`
	// against `tls_cert: {name: "tls-cert"}` must create "tls-cert", not a
	// stray "tls_cert" plus a duplicate registry entry.
	handAuthored := false
	handName := ""
	for _, p := range handPairs {
		if p.Key == o.key || p.Name == o.key {
			handAuthored = true
			handName = p.Name
		}
	}

	// Resolve the entry: existing registration wins unless --force rewrites
	// it; a hand-authored declaration is left where it is (already declared).
	var entry *eval.SecretEntry
	entryIdx := -1
	for i := range entries {
		if entries[i].Key == o.key || entries[i].Name == o.key {
			entryIdx = i
			e := entries[i]
			entry = &e
		}
	}
	flagged := o.length != 0 || o.charset != "" || o.manual || o.podmanName != ""
	if entry != nil && flagged && !o.force {
		return fmt.Errorf("%q is already registered; --force changes its policy, or drop the flags to use the recorded one", o.key)
	}

	desired := eval.SecretEntry{Key: o.key, Name: o.key}
	if o.podmanName != "" {
		desired.Name = o.podmanName
	}
	switch {
	case o.manual:
		// manual: no Generate
	case o.length != 0 || o.charset != "":
		g := &eval.SecretGenerate{Length: defaultSecretLength}
		if o.length != 0 {
			g.Length = o.length
		}
		if o.charset != "" {
			g.Charset = o.charset
		}
		desired.Generate = g
	}

	// The effective entry: recorded (no force) > flags > decided at the prompt.
	eff := desired
	if entry != nil && (!flagged || !o.force) {
		eff = *entry
	}
	if handAuthored && o.podmanName == "" {
		eff.Name = handName
	}

	// Value first, so an aborted prompt registers nothing.
	name := eff.Name
	if o.existing[name] && !o.replace {
		fmt.Fprintf(out, "%s already exists in podman, skipping (use --replace to overwrite)\n", name)
		// Still record the registration when it is new: declaring an existing
		// secret is how adoption into the registry starts — with or without
		// policy flags (an unflagged create records a manual entry).
		if entry == nil && !handAuthored {
			if err := registerSecret(out, projectDir, entries, entryIdx, eff); err != nil {
				return err
			}
		}
		return nil
	}
	var value []byte
	promptGenerated := false
	how := ""
	switch {
	case eff.Generate != nil:
		value, err = generateFromPolicy(eff.Generate)
		if err != nil {
			return fmt.Errorf("%s: %w", o.key, err)
		}
		how = fmt.Sprintf("generated %d chars", len(value))
	default:
		// Manual (flagged or recorded) or undecided: the prompt chooses. A
		// generate choice at the prompt becomes a recorded policy below, so
		// rotate can regenerate it; an entered value stays a manual entry.
		value, promptGenerated, err = secretValuer(o.key)
		if err != nil {
			return fmt.Errorf("%w (pass --length to generate non-interactively)", err)
		}
		if promptGenerated && !o.manual {
			eff.Generate = &eval.SecretGenerate{Length: len(value)}
			how = fmt.Sprintf("generated %d chars", len(value))
		} else {
			how = "value entered"
			if promptGenerated {
				how = "value generated once (manual entry; rotate cannot regenerate it)"
			}
		}
	}

	// Commit the registry update before touching podman: creating (and above
	// all --replace-ing) the secret value is the irreversible half, while the
	// registry is declarative — a declared-but-missing secret is a supported
	// state that `secret create` / `-a` reconciles. So an unwritable (or
	// immutable) registries/secrets.cue aborts while podman is unchanged, and
	// a podman failure afterwards leaves a consistent "declared, not yet
	// created" project rather than a changed secret nobody recorded.
	if !handAuthored {
		staged, err := stageSecretRegistration(projectDir, entries, entryIdx, eff)
		if err != nil {
			return err
		}
		if staged != "" {
			if err := commitSecretRegistration(out, projectDir, staged, eff); err != nil {
				return err
			}
		}
	}
	if err := podmanCreateSecret(name, value, o.replace); err != nil {
		if !handAuthored {
			// With --replace the old secret still exists, so a plain re-run
			// (and -a) would just skip it: only the same --replace finishes
			// the job.
			retry := fmt.Sprintf("re-run 'crei secret create %s' (or -a) to create the value", o.key)
			if o.replace {
				retry = fmt.Sprintf("re-run 'crei secret create %s --replace' to finish the replacement", o.key)
			}
			return fmt.Errorf("%w\n  the entry stays registered in registries/secrets.cue; %s", err, retry)
		}
		return err
	}
	fmt.Fprintf(out, "%s created (%s)\n", green(name), how)
	// A generated value crei cannot regenerate (manual entry, or declared only
	// in the hand-authored field) is shown once; policy-backed values are not
	// (rotate regenerates them).
	if promptGenerated && eff.Generate == nil {
		fmt.Fprintln(out, dim("  save this value now; it will not be shown again:"))
		fmt.Fprintf(out, "  %s\n", string(value))
	}
	return nil
}

// registerSecret upserts the entry into registries/secrets.cue and reports it.
// A no-op when the entry is already recorded identically.
func registerSecret(out io.Writer, projectDir string, entries []eval.SecretEntry, idx int, e eval.SecretEntry) error {
	staged, err := stageSecretRegistration(projectDir, entries, idx, e)
	if err != nil || staged == "" {
		return err
	}
	return commitSecretRegistration(out, projectDir, staged, e)
}

// stageSecretRegistration writes the would-be registries/secrets.cue to a
// staging file in the same directory and returns its path ("" when the entry
// is already recorded identically). Staging up front proves the registry is
// writable before any podman mutation; committing is then a rename.
func stageSecretRegistration(projectDir string, entries []eval.SecretEntry, idx int, e eval.SecretEntry) (string, error) {
	if idx >= 0 {
		prev := entries[idx]
		samePolicy := (prev.Generate == nil) == (e.Generate == nil)
		if samePolicy && prev.Generate != nil {
			samePolicy = *prev.Generate == *e.Generate
		}
		if prev.Name == e.Name && samePolicy {
			return "", nil
		}
		updated := make([]eval.SecretEntry, len(entries))
		copy(updated, entries)
		updated[idx] = e
		entries = updated
	} else {
		entries = append(entries[:len(entries):len(entries)], e)
	}
	content, err := emitSecretRegistry(entries)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(projectDir, "registries")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	// The commit is a rename over secrets.cue, which fails on a non-regular
	// destination (a directory). Detect that now, while podman is untouched.
	dest := filepath.Join(dir, "secrets.cue")
	if fi, err := os.Lstat(dest); err == nil && !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s exists but is not a regular file (%s); refusing to replace it", dest, fi.Mode())
	}
	// A unique temp name so two concurrent crei runs can't clobber each
	// other's staged content.
	tmp, err := os.CreateTemp(dir, ".secrets.cue.*.staged")
	if err != nil {
		return "", fmt.Errorf("stage registry update: %w", err)
	}
	staged := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(staged)
		return "", fmt.Errorf("stage %s: %w", staged, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(staged)
		return "", fmt.Errorf("stage %s: %w", staged, err)
	}
	if err := os.Chmod(staged, 0o644); err != nil {
		_ = os.Remove(staged)
		return "", fmt.Errorf("stage %s: %w", staged, err)
	}
	return staged, nil
}

// commitSecretRegistration renames the staged file into place and reports it.
func commitSecretRegistration(out io.Writer, projectDir string, staged string, e eval.SecretEntry) error {
	path := filepath.Join(projectDir, "registries", "secrets.cue")
	if err := os.Rename(staged, path); err != nil {
		// Clean up the uniquely-named staging file so failed attempts don't
		// accumulate *.staged litter next to the registry.
		_ = os.Remove(staged)
		return fmt.Errorf("write %s: %w", path, err)
	}
	how := "manual"
	if g := e.Generate; g != nil {
		how = fmt.Sprintf("generate %d", g.Length)
		if g.Charset != "" {
			how += " " + g.Charset
		}
	}
	fmt.Fprintf(out, "  registered in registries/secrets.cue (%s)\n", how)
	return nil
}

// createAllMissing walks every declared secret (crei-owned registry plus the
// hand-authored field) absent from podman: policy entries generate, the rest
// prompt. No registration happens; -a acts only on what is already declared.
func createAllMissing(out io.Writer, cfg config, existing map[string]bool, replace bool) error {
	declared, err := declaredSecretNames(cfg)
	if err != nil {
		return err
	}
	policies, err := secretPolicies(cfg)
	if err != nil {
		return err
	}
	missing := 0
	for _, name := range declared {
		if existing[name] {
			continue
		}
		missing++
		var value []byte
		promptGenerated := false
		how := ""
		if g := policies[name]; g != nil {
			value, err = generateFromPolicy(g)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			how = fmt.Sprintf("generated %d chars", len(value))
		} else {
			value, promptGenerated, err = secretValuer(name)
			if err != nil {
				return err
			}
			how = "value entered"
			if promptGenerated {
				how = "value generated"
			}
		}
		if err := podmanCreateSecret(name, value, replace); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s created (%s)\n", green(name), how)
		if promptGenerated {
			fmt.Fprintln(out, dim("  save this value now; it will not be shown again:"))
			fmt.Fprintf(out, "  %s\n", string(value))
		}
	}
	if missing == 0 {
		fmt.Fprintln(out, "All registry secrets already exist in podman.")
	}
	return nil
}
