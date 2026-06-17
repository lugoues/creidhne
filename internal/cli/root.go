// Package cli implements the crei command-line interface.
package cli

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/term"

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
		newConfigCmd(),
		newVersionCmd(),
	)
	return root
}

// --- configuration: flags > env > crei.toml > defaults ---

type config struct {
	ProjectDir string
	QuadletDir string
	DiffTool   string
	// ReloadSystemd is the default for `apply`'s systemctl daemon-reload (on,
	// matching `podman quadlet install --reload-systemd`, unless crei.toml sets
	// it false); the --reload-systemd flag overrides it per-run.
	ReloadSystemd bool

	// Provenance for `crei config`, which layer supplied each value.
	quadletDirSource    string
	diffToolSource      string
	reloadSystemdSource string
	configFilePath      string // crei.toml path if present, else ""
}

type fileConfig struct {
	QuadletDir    string `toml:"quadlet_dir"`
	DiffTool      string `toml:"diff_tool"`
	ReloadSystemd *bool  `toml:"reload_systemd"` // pointer: distinguish unset from false
}

// sourcedValue is a candidate config value paired with a human label for where
// it came from, used to resolve precedence while remembering the winning source.
type sourcedValue struct {
	value, source string
}

// pickSourced returns the first candidate with a non-empty value; if none has a
// value it returns the last candidate (the fallback/default sentinel), so its
// source label is still reported.
func pickSourced(cands ...sourcedValue) sourcedValue {
	for _, c := range cands {
		if c.value != "" {
			return c
		}
	}
	if n := len(cands); n > 0 {
		return cands[n-1]
	}
	return sourcedValue{}
}

func resolveConfig() (config, error) {
	fc, fcPath, err := loadConfigFile(flagProjectDir)
	if err != nil {
		return config{}, err
	}
	qd := pickSourced(
		sourcedValue{flagQuadletDir, "--quadlet-dir flag"},
		sourcedValue{os.Getenv("QUADLET_DIR"), "$QUADLET_DIR"},
		sourcedValue{fc.QuadletDir, "crei.toml"},
		sourcedValue{"~/.config/containers/systemd", "default"},
	)
	expanded, err := expandHome(qd.value)
	if err != nil {
		return config{}, err
	}
	dt := pickSourced(
		sourcedValue{flagDiffTool, "--diff-tool flag"},
		sourcedValue{os.Getenv("DIFF_TOOL"), "$DIFF_TOOL"},
		sourcedValue{fc.DiffTool, "crei.toml"},
		sourcedValue{"", "built-in"},
	)
	reload, reloadSource := true, "default" // matches podman quadlet install
	if fc.ReloadSystemd != nil {
		reload, reloadSource = *fc.ReloadSystemd, "crei.toml"
	}
	return config{
		ProjectDir:          flagProjectDir,
		QuadletDir:          expanded,
		DiffTool:            dt.value,
		ReloadSystemd:       reload,
		quadletDirSource:    qd.source,
		diffToolSource:      dt.source,
		reloadSystemdSource: reloadSource,
		configFilePath:      fcPath,
	}, nil
}

// loadConfigFile reads crei.toml from projectDir. A missing file is fine (zero
// config, empty path); a present-but-malformed file is a hard error rather than
// being silently ignored. Otherwise a typo would route apply to the default
// directory with no warning. The returned path (when the file exists) lets
// `crei config` report which file was loaded.
func loadConfigFile(projectDir string) (fileConfig, string, error) {
	var fc fileConfig
	path := filepath.Join(projectDir, "crei.toml")
	if _, err := os.Stat(path); err != nil {
		return fc, "", nil
	}
	if _, err := toml.DecodeFile(path, &fc); err != nil {
		return fc, path, fmt.Errorf("parse %s: %w", path, err)
	}
	return fc, path, nil
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

// Colors are rendered through lipgloss, which detects the output's color profile
// and honors NO_COLOR / non-TTY automatically (rendering plain text when color
// is unavailable), so callers need no useColor guard.
var (
	greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	redStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	dimStyle    = lipgloss.NewStyle().Faint(true)
)

func green(s string) string  { return greenStyle.Render(s) }
func yellow(s string) string { return yellowStyle.Render(s) }
func red(s string) string    { return redStyle.Render(s) }
func dim(s string) string    { return dimStyle.Render(s) }

// confirm asks for a y/N answer. On an interactive terminal it uses a huh
// prompt; otherwise (piped stdin, CI, tests) it reads a line and returns an
// error when no answer can be read, so a non-interactive run fails loudly
// instead of silently treating "no input" as "no".
func confirm(in io.Reader, out io.Writer, prompt string) (bool, error) {
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		yes := false
		if err := huh.NewConfirm().Title(prompt).Value(&yes).Run(); err != nil {
			return false, err
		}
		return yes, nil
	}
	fmt.Fprintf(out, "%s [y/N] ", prompt)
	sc := bufio.NewScanner(in)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return false, err
		}
		return false, fmt.Errorf("no confirmation read from stdin; re-run with -y to apply non-interactively")
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes", nil
}
