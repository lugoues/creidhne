package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne"
	"github.com/lugoues/creidhne/internal/eval"
)

// Build metadata, overridable via -ldflags "-X .../internal/cli.version=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("crei %s (commit %s, built %s)\n", version, commit, date)
		},
	}
}

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the project's CUE without rendering or writing",
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
			// Strict whole-package check: everything must be concrete and
			// constraint-valid, not just the rendered unit data.
			if err := eval.Validate(cfg.ProjectDir, overlay); err != nil {
				return err
			}
			quads, err := eval.LoadAndValidate(cfg.ProjectDir, overlay)
			if err != nil {
				return err
			}
			units := 0
			for _, q := range quads {
				units += len(q.Units)
			}
			fmt.Printf("OK: %d quadlet(s), %d unit(s) valid\n", len(quads), units)
			return nil
		},
	}
}

func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show the resolved configuration and where each value came from",
		Long: "config prints the effective settings after applying precedence\n" +
			"(flags > env > crei.toml > defaults), annotated with the source of\n" +
			"each value. It evaluates no CUE and writes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			printConfig(cfg)
			return nil
		},
	}
}

// printConfig renders the resolved settings as an aligned table. Only the final
// (source) column is colored, so the ANSI codes don't disturb tabwriter's
// width accounting for the value column.
func printConfig(cfg config) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	row := func(label, value, source string) {
		fmt.Fprintf(w, "  %s\t%s\t%s\n", label, value, dim("("+source+")"))
	}

	projSource := "--dir flag"
	if cfg.ProjectDir == "." {
		projSource = "default"
	}
	row("project dir", cfg.ProjectDir, projSource)
	row("quadlet dir", cfg.QuadletDir, cfg.quadletDirSource)

	diff := cfg.DiffTool
	if diff == "" {
		diff = "built-in unified diff"
	}
	row("diff tool", diff, cfg.diffToolSource)

	reload := "no"
	if cfg.ReloadSystemd {
		reload = "yes"
	}
	row("reload systemd", reload, cfg.reloadSystemdSource)

	scope, reason := "system", "quadlet dir outside $HOME"
	if underHome(cfg.QuadletDir) {
		scope, reason = "--user", "quadlet dir under $HOME"
	}
	row("reload scope", scope, reason)

	cfgFile, cfgSource := cfg.configFilePath, "loaded"
	if cfgFile == "" {
		cfgFile, cfgSource = filepath.Join(cfg.ProjectDir, "crei.toml"), "not found"
	}
	row("config file", cfgFile, cfgSource)

	_ = w.Flush()
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new Creidhne project (cue.mod, sample, config) in --dir",
		Long: "init creates a cue.mod, a sample quadlet, and a crei.toml, and\n" +
			"vendors the embedded schema under cue.mod/usr so editors and the cue\n" +
			"CLI resolve the import without a registry. Existing files are kept.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(flagProjectDir)
		},
	}
}

func runInit(projectDir string) error {
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}
	module := moduleNameFor(projectDir)

	created, err := writeIfAbsent(filepath.Join(projectDir, "cue.mod", "module.cue"),
		fmt.Sprintf("module: %q\nlanguage: {\n\tversion: \"v0.16.0\"\n}\n", module))
	if err != nil {
		return err
	}
	report(created, "cue.mod/module.cue")

	created, err = writeIfAbsent(filepath.Join(projectDir, "main.cue"), sampleMain)
	if err != nil {
		return err
	}
	report(created, "main.cue")

	created, err = writeIfAbsent(filepath.Join(projectDir, "crei.toml"), sampleConfig)
	if err != nil {
		return err
	}
	report(created, "crei.toml")

	if err := vendorSchema(projectDir); err != nil {
		return fmt.Errorf("vendor schema: %w", err)
	}
	fmt.Printf("  %s cue.mod/usr/%s (vendored schema for editor/LSP)\n", green("✓"), eval.ModulePath)

	fmt.Println("\nNext: edit main.cue, then run 'crei plan'.")
	return nil
}

// vendoredSchemaDir is where the embedded schema is materialized on disk for
// on-disk tooling (cue vet, the editor LSP) to resolve the import.
func vendoredSchemaDir(moduleRoot string) string {
	return filepath.Join(moduleRoot, "cue.mod", "usr", filepath.FromSlash(eval.ModulePath))
}

// vendorSchema writes the embedded schema module to <moduleRoot>/cue.mod/usr/
// <ModulePath>/ so on-disk tooling resolves the import. The binary itself uses
// the embedded copy directly.
func vendorSchema(moduleRoot string) error {
	return writeSchemaTo(vendoredSchemaDir(moduleRoot))
}

// writeSchemaTo materializes the embedded schema tree under base.
func writeSchemaTo(base string) error {
	return fs.WalkDir(creidhne.SchemaFS, "creidhne", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, "creidhne"), "/")
		dest := filepath.Join(base, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		b, err := fs.ReadFile(creidhne.SchemaFS, p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, b, 0o644)
	})
}

// syncVendoredSchema refreshes an existing on-disk vendored schema when it has
// drifted from this binary's embedded copy, so the editor LSP / cue CLI always
// validate against exactly what crei renders (e.g. after upgrading the binary).
// It is best-effort and LSP-only. The binary resolves the schema from the
// embedded overlay regardless, so errors (a read-only dir, etc.) are ignored.
// It never *creates* a vendored copy, only refreshes one, leaving projects that
// resolve the schema another way (a registry dependency) untouched. A symlinked
// vendored copy (the dev/example layout that points at live source) is left
// alone, and the refresh is staged in a temp dir then swapped in, so a partial
// write never corrupts the live copy.
func syncVendoredSchema(moduleRoot string) {
	vendorDir := vendoredSchemaDir(moduleRoot)
	fi, err := os.Lstat(vendorDir)
	if err != nil {
		return // not vendored, so respect the project's setup
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return // symlinked to live source, so never clobber the dev layout
	}
	if vendoredMatchesEmbedded(vendorDir) {
		return // already in sync
	}
	staging := vendorDir + ".crei-tmp"
	_ = os.RemoveAll(staging)
	if err := writeSchemaTo(staging); err != nil {
		_ = os.RemoveAll(staging) // leave the live copy untouched on a failed write
		return
	}
	if err := os.RemoveAll(vendorDir); err != nil {
		_ = os.RemoveAll(staging)
		return
	}
	if err := os.Rename(staging, vendorDir); err != nil {
		return
	}
	fmt.Fprintln(os.Stderr, dim("refreshed vendored schema in cue.mod/usr to match this crei build"))
}

// vendoredMatchesEmbedded reports whether the vendored copy is exactly the
// embedded schema: every embedded file present and byte-identical, AND no extra
// on-disk files (a schema file removed in a newer binary must not linger, or the
// LSP keeps seeing a type the binary no longer ships).
func vendoredMatchesEmbedded(vendorDir string) bool {
	expected := map[string]bool{}
	match := true
	_ = fs.WalkDir(creidhne.SchemaFS, "creidhne", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(p, "creidhne"), "/"))
		expected[rel] = true
		want, err := fs.ReadFile(creidhne.SchemaFS, p)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(vendorDir, rel))
		if err != nil || !bytes.Equal(got, want) {
			match = false
		}
		return nil
	})
	if !match {
		return false
	}
	// Any vendored file not in the embedded set is stale drift.
	_ = filepath.WalkDir(vendorDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(vendorDir, p)
		if err != nil {
			return err
		}
		if !expected[rel] {
			match = false
			return filepath.SkipAll
		}
		return nil
	})
	return match
}

func writeIfAbsent(path, content string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(content), 0o644)
}

func report(created bool, name string) {
	if created {
		fmt.Printf("  %s %s\n", green("✓"), name)
	} else {
		fmt.Printf("  %s %s (exists, kept)\n", dim("-"), name)
	}
}

// moduleNameFor derives a domain-shaped CUE module path from the project dir.
func moduleNameFor(projectDir string) string {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		abs = projectDir
	}
	base := strings.ToLower(filepath.Base(abs))
	var b strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		name = "config"
	}
	return "example.com/" + name + "@v0"
}

const sampleMain = `package config

import "github.com/lugoues/creidhne@v0"

// A minimal example. Run 'crei plan' to preview, 'crei apply' to write.
hello: creidhne.#Quadlet & {
	name: "hello"
	units: #container: {
		Container: {
			Image:         "docker.io/library/hello-world:latest"
			ContainerName: "hello"
		}
		Install: WantedBy: ["default.target"]
	}
}
`

const sampleConfig = `# Target directory for generated quadlet unit files.
quadlet_dir = "~/.config/containers/systemd"

# Optional external diff tool (e.g. "delta"); empty uses the built-in differ.
# diff_tool = ""

# Run 'systemctl daemon-reload' after 'crei apply' by default. Off unless set
# here; override per-run with --reload-systemd[=false].
# reload_systemd = false
`
