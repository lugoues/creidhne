// Package cli implements the crei command-line interface.
package cli

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"cuelang.org/go/cue/load"

	"github.com/lugoues/creidhne"
	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/reconcile"
	"github.com/lugoues/creidhne/internal/render"
)

var (
	flagProjectDir string
	flagQuadletDir string
	flagDiffTool   string
)

// Execute runs the root command and exits non-zero on error.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, red("Error: "+err.Error()))
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "creidhne",
		Short: "Generate and apply Podman Quadlet systemd units from CUE",
		Long: "Creidhne renders Podman Quadlet unit files from typed, validated CUE\n" +
			"definitions and reconciles them against a quadlet directory.\n\n" +
			"The CUE schema is embedded in this binary, so projects resolve\n" +
			"`import \"github.com/lugoues/creidhne@v0\"` offline.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&flagProjectDir, "dir", "C", ".", "project directory containing CUE files")
	pf.StringVar(&flagQuadletDir, "quadlet-dir", "", "target quadlet directory (overrides $QUADLET_DIR and config)")
	pf.StringVar(&flagDiffTool, "diff-tool", "", "external diff tool to use (default: built-in unified diff)")
	root.AddCommand(
		newRenderCmd(),
		newPlanCmd(),
		newDiffCmd(),
		newApplyCmd(),
		newInitCmd(),
		newValidateCmd(),
		newVersionCmd(),
	)
	return root
}

// --- configuration: flags > env > crei.toml > defaults ---

type config struct {
	ProjectDir string
	QuadletDir string
	DiffTool   string
}

type fileConfig struct {
	QuadletDir string `toml:"quadlet_dir"`
	DiffTool   string `toml:"diff_tool"`
}

func resolveConfig() (config, error) {
	fc := loadConfigFile(flagProjectDir)
	quadletDir := firstNonEmpty(flagQuadletDir, os.Getenv("QUADLET_DIR"), fc.QuadletDir, "~/.config/containers/systemd")
	expanded, err := expandHome(quadletDir)
	if err != nil {
		return config{}, err
	}
	return config{
		ProjectDir: flagProjectDir,
		QuadletDir: expanded,
		DiffTool:   firstNonEmpty(flagDiffTool, os.Getenv("DIFF_TOOL"), fc.DiffTool),
	}, nil
}

// loadConfigFile reads crei.toml from projectDir (best effort: missing or
// malformed files yield a zero config).
func loadConfigFile(projectDir string) fileConfig {
	var fc fileConfig
	path := filepath.Join(projectDir, "crei.toml")
	if _, err := os.Stat(path); err != nil {
		return fc
	}
	_, _ = toml.DecodeFile(path, &fc)
	return fc
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
}

// underHome reports whether dir is within the user's home directory. Used to
// pick `systemctl --user` (rootless, dir under $HOME) vs system scope for
// daemon-reload, a path heuristic, not a privilege check.
func underHome(dir string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(home, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// --- generation pipeline (eval + render) ---

// buildOverlay resolves the schema import for projectDir from the embedded
// module via an overlay (offline, version-locked to this binary) and keeps the
// on-disk vendored copy in lockstep. Shared by the load and validate paths.
func buildOverlay(projectDir string) (map[string]load.Source, error) {
	moduleRoot, err := eval.FindModuleRoot(projectDir)
	if err != nil {
		return nil, err
	}
	// Keep the on-disk vendored schema (LSP/cue-CLI) in lockstep with this
	// binary's embedded copy; no-op unless it has drifted. Best-effort.
	syncVendoredSchema(moduleRoot)
	return eval.Overlay(moduleRoot, creidhne.SchemaFS)
}

// loadQuadlets evaluates the project's CUE and extracts its quadlets.
func loadQuadlets(projectDir string) ([]eval.Quadlet, error) {
	overlay, err := buildOverlay(projectDir)
	if err != nil {
		return nil, err
	}
	return eval.LoadAndValidate(projectDir, overlay)
}

// generate evaluates and renders the project into the desired file set.
func generate(projectDir string) (map[string]reconcile.DesiredFile, error) {
	quads, err := loadQuadlets(projectDir)
	if err != nil {
		return nil, err
	}
	if len(quads) == 0 {
		return nil, fmt.Errorf("no quadlets found (no top-level #Quadlet values in %s)", projectDir)
	}
	tplFS, err := fs.Sub(creidhne.TemplatesFS, "templates")
	if err != nil {
		return nil, err
	}
	r, err := render.New(tplFS)
	if err != nil {
		return nil, err
	}
	files, err := r.BuildFileSet(quads)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no quadlet files generated")
	}
	desired := make(map[string]reconcile.DesiredFile, len(files))
	for name, fc := range files {
		desired[name] = reconcile.DesiredFile{Content: fc.Content, Mode: fc.Mode}
	}
	return desired, nil
}

func sortedKeys(m map[string]reconcile.DesiredFile) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}

// --- output helpers ---

var useColor = isTTY(os.Stdout) && os.Getenv("NO_COLOR") == ""

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func colorize(code, s string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func green(s string) string  { return colorize("32", s) }
func yellow(s string) string { return colorize("33", s) }
func red(s string) string    { return colorize("31", s) }
func dim(s string) string    { return colorize("2", s) }

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}
