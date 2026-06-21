package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUnderHomeResolvesSymlinkedHome guards the daemon-reload scope heuristic
// against a symlinked $HOME. When $HOME is a symlink and the quadlet dir is
// expressed via its resolved (physical) path, a purely lexical comparison judges
// the dir to be outside $HOME and picks system scope instead of `--user`, so the
// rootless units never get reloaded. underHome must resolve symlinks on both
// sides.
func TestUnderHomeResolvesSymlinkedHome(t *testing.T) {
	real := t.TempDir()
	cfg := filepath.Join(real, ".config", "containers", "systemd")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	t.Setenv("HOME", link)

	// dir given via the resolved path while $HOME is the symlink.
	if !underHome(cfg) {
		t.Errorf("underHome(%q) = false with symlinked HOME=%q; want true", cfg, link)
	}
	// A dir not yet created (first apply) under the symlinked home must also match.
	notYet := filepath.Join(cfg, "subdir-not-created")
	if !underHome(notYet) {
		t.Errorf("underHome(%q) = false for not-yet-existing dir under symlinked HOME; want true", notYet)
	}
}

// TestUnderHomeRejectsOutsideHome is the non-regression companion: a dir clearly
// outside $HOME must still be judged system scope.
func TestUnderHomeRejectsOutsideHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	outside := t.TempDir() // a sibling temp dir, not under home
	if underHome(outside) {
		t.Errorf("underHome(%q) = true for a dir outside HOME=%q; want false", outside, home)
	}
}
