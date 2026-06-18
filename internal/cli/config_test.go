package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/lugoues/creidhne"
)

// TestResolveConfigProvenance checks the precedence chain (flag > env >
// crei.toml > default) and that the winning source is recorded for `crei config`.
func TestResolveConfigProvenance(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "crei.toml"),
		[]byte("quadlet_dir = \"/srv/from-toml\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reset package-level flag vars after the test.
	defer func() { flagProjectDir, flagQuadletDir, flagDiffTool = ".", "", "" }()
	flagProjectDir, flagQuadletDir, flagDiffTool = dir, "", ""
	t.Setenv("QUADLET_DIR", "")
	t.Setenv("DIFF_TOOL", "")

	// crei.toml wins for quadlet_dir; diff tool falls back to built-in.
	cfg, err := resolveConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuadletDir != "/srv/from-toml" || cfg.quadletDirSource != "crei.toml" {
		t.Fatalf("toml: dir=%q source=%q", cfg.QuadletDir, cfg.quadletDirSource)
	}
	if cfg.DiffTool != "" || cfg.diffToolSource != "built-in" {
		t.Fatalf("diff: tool=%q source=%q", cfg.DiffTool, cfg.diffToolSource)
	}
	if cfg.configFilePath == "" {
		t.Fatal("configFilePath should be set when crei.toml exists")
	}

	// env overrides crei.toml.
	t.Setenv("QUADLET_DIR", "/srv/from-env")
	if cfg, _ = resolveConfig(); cfg.QuadletDir != "/srv/from-env" || cfg.quadletDirSource != "$QUADLET_DIR" {
		t.Fatalf("env: dir=%q source=%q", cfg.QuadletDir, cfg.quadletDirSource)
	}

	// flag overrides env.
	flagQuadletDir = "/srv/from-flag"
	if cfg, _ = resolveConfig(); cfg.quadletDirSource != "--quadlet-dir flag" {
		t.Fatalf("flag: source=%q", cfg.quadletDirSource)
	}
}

// TestResolveConfigReloadSystemd: reload defaults on, is taken from crei.toml
// when set (true or false), with the source recorded.
func TestResolveConfigReloadSystemd(t *testing.T) {
	defer func() { flagProjectDir, flagQuadletDir, flagDiffTool = ".", "", "" }()
	flagQuadletDir, flagDiffTool = "", ""
	t.Setenv("QUADLET_DIR", "")
	t.Setenv("DIFF_TOOL", "")

	cases := []struct {
		name       string
		toml       string // "" => no crei.toml
		wantReload bool
		wantSource string
	}{
		{"default", "", true, "default"}, // on, matching podman quadlet install
		{"toml true", "reload_systemd = true\n", true, "crei.toml"},
		{"toml false", "reload_systemd = false\n", false, "crei.toml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.toml != "" {
				if err := os.WriteFile(filepath.Join(dir, "crei.toml"), []byte(c.toml), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			flagProjectDir = dir
			cfg, err := resolveConfig()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ReloadSystemd != c.wantReload || cfg.reloadSystemdSource != c.wantSource {
				t.Fatalf("reload=%v source=%q; want %v/%q", cfg.ReloadSystemd, cfg.reloadSystemdSource, c.wantReload, c.wantSource)
			}
		})
	}
}

// TestResolveConfigMalformedToml: a present-but-unparseable crei.toml is a hard
// error (not silently ignored, which would route apply to the default dir),
// while a missing crei.toml is fine.
func TestResolveConfigMalformedToml(t *testing.T) {
	defer func() { flagProjectDir, flagQuadletDir, flagDiffTool = ".", "", "" }()
	flagQuadletDir, flagDiffTool = "", ""
	t.Setenv("QUADLET_DIR", "")
	t.Setenv("DIFF_TOOL", "")

	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "crei.toml"), []byte("quadlet_dir = /unquoted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	flagProjectDir = bad
	if _, err := resolveConfig(); err == nil {
		t.Fatal("malformed crei.toml should error, not be silently ignored")
	}

	flagProjectDir = t.TempDir() // no crei.toml
	if _, err := resolveConfig(); err != nil {
		t.Fatalf("missing crei.toml should be fine, got %v", err)
	}
}

// TestResolveConfigInvalidDiffStyle: an unknown diff_style is a hard error, not
// silently coerced to the default.
func TestResolveConfigInvalidDiffStyle(t *testing.T) {
	defer func() { flagProjectDir, flagQuadletDir, flagDiffTool = ".", "", "" }()
	flagQuadletDir, flagDiffTool = "", ""
	t.Setenv("QUADLET_DIR", "")
	t.Setenv("DIFF_TOOL", "")

	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "crei.toml"), []byte("diff_style = \"fancy\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	flagProjectDir = bad
	if _, err := resolveConfig(); err == nil {
		t.Fatal("invalid diff_style should error")
	}
}

// TestResolveConfigDefaults: with nothing set, quadlet_dir comes from the
// default (and ~ is expanded), and no config file is reported.
func TestResolveConfigDefaults(t *testing.T) {
	defer func() { flagProjectDir, flagQuadletDir, flagDiffTool = ".", "", "" }()
	flagProjectDir, flagQuadletDir, flagDiffTool = t.TempDir(), "", ""
	t.Setenv("QUADLET_DIR", "")
	t.Setenv("DIFF_TOOL", "")

	cfg, err := resolveConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.quadletDirSource != "default" {
		t.Fatalf("source = %q, want default", cfg.quadletDirSource)
	}
	if strings.HasPrefix(cfg.QuadletDir, "~") || !strings.HasSuffix(cfg.QuadletDir, filepath.FromSlash(".config/containers/systemd")) {
		t.Fatalf("default dir not expanded: %q", cfg.QuadletDir)
	}
	if cfg.configFilePath != "" {
		t.Fatalf("no crei.toml expected, got %q", cfg.configFilePath)
	}
}

// TestApplyStyles: [style] overrides land on the right styles, unset entries
// fall back to defaults, and the inline-span colors inherit their line color
// unless given their own.
func TestApplyStyles(t *testing.T) {
	t.Cleanup(func() { applyStyles(styleConfig{}) }) // restore defaults

	applyStyles(styleConfig{
		Add:           styleSpec{Fg: "#abcdef", set: true},
		RemoveChar:    styleSpec{Fg: "#123456", set: true},
		Header:        styleSpec{Fg: "#ffffff", set: true},
		InlineContext: styleSpec{Fg: "#fedcba", set: true},
	})

	if got := greenStyle.GetForeground(); got != lipgloss.Color("#abcdef") {
		t.Errorf("add color = %v, want #abcdef", got)
	}
	if got := addSpanStyle.GetForeground(); got != lipgloss.Color("#abcdef") {
		t.Errorf("add_char (unset) should inherit add, got %v", got)
	}
	if got := delSpanStyle.GetForeground(); got != lipgloss.Color("#123456") {
		t.Errorf("remove_char = %v, want #123456", got)
	}
	if got := redStyle.GetForeground(); got != lipgloss.Color(colorRemove) {
		t.Errorf("remove (unset) should be the default %s, got %v", colorRemove, got)
	}
	if got := diffHeaderStyle.GetForeground(); got != lipgloss.Color("#ffffff") {
		t.Errorf("header color = %v, want #ffffff", got)
	}
	if got := inlineContextStyle.GetForeground(); got != lipgloss.Color("#fedcba") {
		t.Errorf("inline_context color = %v, want #fedcba", got)
	}
	if got := diffContextStyle.GetForeground(); got != lipgloss.Color(colorContext) {
		t.Errorf("context (unset) should stay the default %s, got %v", colorContext, got)
	}

	// inline_context, when unset, inherits the text (normal) style.
	applyStyles(styleConfig{Text: styleSpec{Fg: "#0a0b0c", set: true}})
	if got := inlineContextStyle.GetForeground(); got != lipgloss.Color("#0a0b0c") {
		t.Errorf("inline_context (unset) should inherit text, got %v", got)
	}

	// Defaults: a bold (uncolored) header, and normal/inline text at the
	// terminal default (no color).
	applyStyles(styleConfig{})
	if got := diffHeaderStyle.GetForeground(); got != (lipgloss.NoColor{}) {
		t.Errorf("default header should carry no color, got %v", got)
	}
	if got := inlineContextStyle.GetForeground(); got != (lipgloss.NoColor{}) {
		t.Errorf("default inline_context should be the terminal default, got %v", got)
	}
}

// TestStyleConfigParsing: a [style] entry is either a bare color string (fg) or
// a table with fg/bg + attributes; unknown keys and bad color values are
// rejected at parse time.
func TestStyleConfigParsing(t *testing.T) {
	write := func(t *testing.T, body string) (fileConfig, error) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "crei.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		fc, _, err := loadConfigFile(dir)
		return fc, err
	}

	fc, err := write(t, "[style]\nadd = \"#003500\"\nremove = { bg = \"#5e0000\", bold = true }\n")
	if err != nil {
		t.Fatalf("valid [style] should parse: %v", err)
	}
	if !fc.Style.Add.set || fc.Style.Add.Fg != "#003500" {
		t.Errorf("bare string should set fg: %+v", fc.Style.Add)
	}
	if !fc.Style.Remove.set || fc.Style.Remove.Bg != "#5e0000" || !fc.Style.Remove.Bold {
		t.Errorf("table should set bg + bold: %+v", fc.Style.Remove)
	}
	if fc.Style.Add.style().GetForeground() != lipgloss.Color("#003500") {
		t.Error("parsed spec should build the foreground")
	}

	if _, err := write(t, "[style]\nadd = { fgg = \"#fff\" }\n"); err == nil {
		t.Error("unknown style attribute should error")
	}
	if _, err := write(t, "[style]\nadd = \"reddish\"\n"); err == nil {
		t.Error("invalid color value should error")
	}
	if _, err := write(t, "[style]\nadd = { bold = \"yes\" }\n"); err == nil {
		t.Error("non-boolean attribute should error")
	}
}

// TestConfigSchemaCoversFields guards against the embedded crei.schema.json
// drifting from the config structs: every TOML field must appear as a schema
// property, so adding a config key without a schema entry fails here.
func TestConfigSchemaCoversFields(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(creidhne.ConfigSchema, &schema); err != nil {
		t.Fatalf("crei.schema.json is not valid JSON: %v", err)
	}

	for _, tag := range tomlTags(reflect.TypeOf(fileConfig{})) {
		if _, ok := schema.Properties[tag]; !ok {
			t.Errorf("crei.schema.json missing top-level property %q", tag)
		}
	}
	style := schema.Properties["style"].Properties
	for _, tag := range tomlTags(reflect.TypeOf(styleConfig{})) {
		if _, ok := style[tag]; !ok {
			t.Errorf("crei.schema.json [style] missing property %q", tag)
		}
	}
}

func tomlTags(typ reflect.Type) []string {
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		if tag := typ.Field(i).Tag.Get("toml"); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}
