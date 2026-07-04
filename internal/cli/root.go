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
	"strconv"
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
		// Theme the output from crei.toml [style] before any command runs.
		// Best-effort: a malformed file is ignored here (resolveConfig reports it
		// for real commands) so defaults apply and `version` never fails on it.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			fc, _, _ := loadConfigFile(flagProjectDir)
			applyStyles(fc.Style)
			return nil
		},
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
		newGraphCmd(),
		newInitCmd(),
		newValidateCmd(),
		newConfigCmd(),
		newSecretsCmd(),
		newVersionCmd(),
	)
	return root
}

// --- configuration: flags > env > crei.toml > defaults ---

type config struct {
	ProjectDir string
	QuadletDir string
	DiffTool   string
	// DiffStyle selects how modified lines render: highlight, plain, or inline.
	DiffStyle string
	// ReloadSystemd is the default for `apply`'s systemctl daemon-reload (on,
	// matching `podman quadlet install --reload-systemd`, unless crei.toml sets
	// it false); the --reload-systemd flag overrides it per-run.
	ReloadSystemd bool
	// SecretsField is the top-level CUE field `crei secrets` reads the
	// #SecretRegistry from (default "secrets").
	SecretsField string

	// Provenance for `crei config`, which layer supplied each value.
	quadletDirSource    string
	diffToolSource      string
	diffStyleSource     string
	reloadSystemdSource string
	secretsFieldSource  string
	configFilePath      string // crei.toml path if present, else ""
}

type fileConfig struct {
	QuadletDir    string      `toml:"quadlet_dir"`
	DiffTool      string      `toml:"diff_tool"`
	DiffStyle     string      `toml:"diff_style"`
	ReloadSystemd *bool       `toml:"reload_systemd"` // pointer: distinguish unset from false
	SecretsField  string      `toml:"secrets_field"`
	Style         styleConfig `toml:"style"`
}

// diff_style values: how a modified line renders in plan/diff/apply.
const (
	diffStyleHighlight = "highlight" // "- old" / "+ new" pair, changed span highlighted (default)
	diffStylePlain     = "plain"     // "- old" / "+ new" pair, whole lines colored, no span highlight
	diffStyleInline    = "inline"    // single "~" line, word-diff: removed run struck (remove color), added run (add color)
)

// styleConfig overrides the output styles from crei.toml's [style] table. Each
// element is either a bare color string (foreground only) or an inline table
// (fg/bg + attribute toggles); an unset element keeps its built-in default.
type styleConfig struct {
	Header        styleSpec `toml:"header"`         // "# name" file header (default: bold)
	Text          styleSpec `toml:"text"`           // normal text (default: terminal default)
	Context       styleSpec `toml:"context"`        // unchanged context lines
	InlineContext styleSpec `toml:"inline_context"` // unchanged text in a modified row (defaults to text)
	Add           styleSpec `toml:"add"`            // added lines (+)
	Remove        styleSpec `toml:"remove"`         // removed lines (-)
	AddChar       styleSpec `toml:"add_char"`       // added inline span (defaults to add's color)
	RemoveChar    styleSpec `toml:"remove_char"`    // removed inline span (defaults to remove's color)
}

// styleSpec is a configurable lipgloss style. In crei.toml it unmarshals from
// either a color string (foreground only) or an inline table with fg/bg and
// attribute toggles. Colors are hex ("#3FB950"), an ANSI index ("0".."255"), or
// "" for the terminal default; lipgloss degrades them to the terminal's profile.
type styleSpec struct {
	Fg, Bg                                                 string
	Bold, Italic, Underline, Reverse, Strikethrough, Faint bool
	set                                                    bool // present in the config
}

// UnmarshalTOML accepts a bare string (foreground) or a table, rejecting unknown
// keys and unparseable colors so a typo fails loudly instead of rendering plain.
func (s *styleSpec) UnmarshalTOML(v interface{}) error {
	s.set = true
	switch val := v.(type) {
	case string:
		s.Fg = val
		return validateColor("fg", s.Fg)
	case map[string]interface{}:
		for k, raw := range val {
			var err error
			switch k {
			case "fg":
				err = assignColor(&s.Fg, k, raw)
			case "bg":
				err = assignColor(&s.Bg, k, raw)
			case "bold":
				err = assignBool(&s.Bold, k, raw)
			case "italic":
				err = assignBool(&s.Italic, k, raw)
			case "underline":
				err = assignBool(&s.Underline, k, raw)
			case "reverse":
				err = assignBool(&s.Reverse, k, raw)
			case "strikethrough":
				err = assignBool(&s.Strikethrough, k, raw)
			case "faint":
				err = assignBool(&s.Faint, k, raw)
			default:
				err = fmt.Errorf("unknown color attribute %q (want fg, bg, bold, italic, underline, reverse, strikethrough, faint)", k)
			}
			if err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("color must be a string or a table, got %T", v)
	}
}

func (s styleSpec) style() lipgloss.Style {
	st := lipgloss.NewStyle()
	if s.Fg != "" {
		st = st.Foreground(lipgloss.Color(s.Fg))
	}
	if s.Bg != "" {
		st = st.Background(lipgloss.Color(s.Bg))
	}
	return st.Bold(s.Bold).Italic(s.Italic).Underline(s.Underline).
		Reverse(s.Reverse).Strikethrough(s.Strikethrough).Faint(s.Faint)
}

func assignColor(dst *string, key string, v interface{}) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("color attribute %q must be a string", key)
	}
	if err := validateColor(key, s); err != nil {
		return err
	}
	*dst = s
	return nil
}

func assignBool(dst *bool, key string, v interface{}) error {
	b, ok := v.(bool)
	if !ok {
		return fmt.Errorf("color attribute %q must be true or false", key)
	}
	*dst = b
	return nil
}

// validateColor accepts a hex color (#rgb or #rrggbb), an ANSI index 0-255, or
// "" (terminal default) -- the set lipgloss understands. Anything else (a name
// like "red", a typo) renders as no color, so reject it up front.
func validateColor(key, s string) error {
	if s == "" {
		return nil
	}
	if rest, ok := strings.CutPrefix(s, "#"); ok {
		if len(rest) != 3 && len(rest) != 6 {
			return fmt.Errorf("color %q for %q: hex must be #rgb or #rrggbb", s, key)
		}
		if _, err := strconv.ParseUint(rest, 16, 64); err != nil {
			return fmt.Errorf("color %q for %q: invalid hex digits", s, key)
		}
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 255 {
		return nil
	}
	return fmt.Errorf("color %q for %q: want a hex color (#3FB950), an ANSI index 0-255, or empty", s, key)
}

// applyStyles rebuilds the shared output styles from the configured [style]
// table, falling back to the built-in defaults. An unset add_char/remove_char
// inherits its line color. The CLI is single-threaded and this runs before any
// output (root PersistentPreRunE), so mutating the package-level styles is safe.
func applyStyles(c styleConfig) {
	addFg := orElse(c.Add.Fg, colorAdd)
	removeFg := orElse(c.Remove.Fg, colorRemove)
	// "Normal" text defaults to the terminal default; inline context (unchanged
	// text within a modified row) inherits it unless given its own style.
	text := resolveStyle(c.Text, lipgloss.NewStyle())
	greenStyle = resolveStyle(c.Add, lipgloss.NewStyle().Foreground(lipgloss.Color(addFg)))
	redStyle = resolveStyle(c.Remove, lipgloss.NewStyle().Foreground(lipgloss.Color(removeFg)))
	diffContextStyle = resolveStyle(c.Context, lipgloss.NewStyle().Foreground(lipgloss.Color(colorContext)))
	inlineContextStyle = resolveStyle(c.InlineContext, text)
	addSpanStyle = resolveStyle(c.AddChar, lipgloss.NewStyle().Foreground(lipgloss.Color(addFg)).Bold(true))
	delSpanStyle = resolveStyle(c.RemoveChar, lipgloss.NewStyle().Foreground(lipgloss.Color(removeFg)).Bold(true))
	diffHeaderStyle = resolveStyle(c.Header, lipgloss.NewStyle().Bold(true))
}

// resolveStyle returns the configured style if the element was set, else def.
func resolveStyle(spec styleSpec, def lipgloss.Style) lipgloss.Style {
	if spec.set {
		return spec.style()
	}
	return def
}

func orElse(v, def string) string {
	if v == "" {
		return def
	}
	return v
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
	ds := pickSourced(
		sourcedValue{fc.DiffStyle, "crei.toml"},
		sourcedValue{diffStyleHighlight, "default"},
	)
	switch ds.value {
	case diffStyleHighlight, diffStylePlain, diffStyleInline:
	default:
		return config{}, fmt.Errorf("invalid diff_style %q in crei.toml (want %q, %q, or %q)",
			ds.value, diffStyleHighlight, diffStylePlain, diffStyleInline)
	}
	reload, reloadSource := true, "default" // matches podman quadlet install
	if fc.ReloadSystemd != nil {
		reload, reloadSource = *fc.ReloadSystemd, "crei.toml"
	}
	sf := pickSourced(
		sourcedValue{fc.SecretsField, "crei.toml"},
		sourcedValue{"secrets", "default"},
	)
	return config{
		ProjectDir:          flagProjectDir,
		QuadletDir:          expanded,
		DiffTool:            dt.value,
		DiffStyle:           ds.value,
		ReloadSystemd:       reload,
		SecretsField:        sf.value,
		quadletDirSource:    qd.source,
		diffToolSource:      dt.source,
		diffStyleSource:     ds.source,
		reloadSystemdSource: reloadSource,
		secretsFieldSource:  sf.source,
		configFilePath:      fcPath,
	}, nil
}

// loadConfigFile reads the config from projectDir/.crei/config.toml. A missing
// file is fine (zero config, empty path); a present-but-malformed file is a hard
// error rather than being silently ignored. Otherwise a typo would route apply to
// the default directory with no warning. The returned path (when the file exists)
// lets `crei config` report which file was loaded.
func loadConfigFile(projectDir string) (fileConfig, string, error) {
	var fc fileConfig
	path := filepath.Join(projectDir, ".crei", "config.toml")
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
	// Resolve symlinks on both sides before comparing: a symlinked $HOME (e.g.
	// /home/user -> /data/user) with the quadlet dir given via its resolved path
	// would otherwise be judged outside $HOME, picking system scope instead of
	// `--user` so rootless units never reload.
	home = resolveSymlinks(home)
	abs = resolveSymlinks(abs)
	rel, err := filepath.Rel(home, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveSymlinks returns the symlink-resolved path. The quadlet dir may not
// exist yet (first apply), so when EvalSymlinks fails it resolves the deepest
// existing ancestor and re-appends the missing tail, so a symlinked $HOME still
// matches a not-yet-created dir beneath it.
func resolveSymlinks(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	parent := filepath.Dir(p)
	if parent == p { // reached the root; nothing resolved
		return p
	}
	return filepath.Join(resolveSymlinks(parent), filepath.Base(p))
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

// generate evaluates and renders the whole project into the desired file set.
func generate(projectDir string) (map[string]reconcile.DesiredFile, error) {
	quads, err := loadQuadlets(projectDir)
	if err != nil {
		return nil, err
	}
	if len(quads) == 0 {
		return nil, fmt.Errorf("no quadlets found (no top-level #Quadlet values in %s)", projectDir)
	}
	return renderQuadlets(quads)
}

// renderQuadlets renders a set of quadlets into the desired file set. Rendering a
// subset is valid: cross-quadlet references (e.g. After=app.service) are resolved
// into each unit's data at eval time, so a unit renders identically whether or
// not the quadlets it references are in the set.
func renderQuadlets(quads []eval.Quadlet) (map[string]reconcile.DesiredFile, error) {
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

// filterQuadlets returns the quadlets named in wanted, in the order requested and
// deduplicated. An unknown name is an error that lists what is available, so a
// typo fails loudly instead of silently rendering nothing.
func filterQuadlets(quads []eval.Quadlet, wanted []string) ([]eval.Quadlet, error) {
	byName := make(map[string]eval.Quadlet, len(quads))
	available := make([]string, 0, len(quads))
	for _, q := range quads {
		byName[q.Name] = q
		available = append(available, q.Name)
	}
	sort.Strings(available)
	var out []eval.Quadlet
	seen := make(map[string]bool, len(wanted))
	for _, name := range wanted {
		if seen[name] {
			continue
		}
		q, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("no quadlet named %q (available: %s)", name, strings.Join(available, ", "))
		}
		seen[name] = true
		out = append(out, q)
	}
	return out, nil
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
// is unavailable), so callers need no useColor guard. Defaults are truecolor
// hex; lipgloss degrades them to 256-color or basic ANSI on lesser terminals.
const (
	colorAdd     = "#3FB950" // green: added lines / success
	colorChanged = "#D29922" // amber: in-place changes (compact list ~)
	colorRemove  = "#F85149" // red: removed lines / errors
	colorContext = "#6E7681" // gray: unchanged context lines
)

var (
	greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAdd))
	yellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorChanged))
	redStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colorRemove))
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
