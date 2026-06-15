package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
