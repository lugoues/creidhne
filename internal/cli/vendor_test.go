package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lugoues/creidhne/internal/eval"
)

func TestVendorSyncRefreshesStaleSchema(t *testing.T) {
	root := t.TempDir()
	if err := vendorSchema(root); err != nil {
		t.Fatalf("vendor: %v", err)
	}
	vendorDir := filepath.Join(root, "cue.mod", "usr", filepath.FromSlash(eval.ModulePath))

	if !vendoredMatchesEmbedded(vendorDir) {
		t.Fatal("freshly vendored schema should match the embedded copy")
	}

	// Tamper with a vendored file → now drifted.
	victim := filepath.Join(vendorDir, "types.cue")
	if err := os.WriteFile(victim, []byte("package creidhne // tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if vendoredMatchesEmbedded(vendorDir) {
		t.Fatal("tampered schema should not match the embedded copy")
	}

	// sync detects the drift and restores it.
	syncVendoredSchema(root)
	if !vendoredMatchesEmbedded(vendorDir) {
		t.Fatal("sync should have restored the vendored schema")
	}
}

func TestVendorSyncSkipsWhenAbsent(t *testing.T) {
	root := t.TempDir()
	// No vendored copy present → sync must not create one (respects projects
	// that resolve the schema from a registry instead).
	syncVendoredSchema(root)
	if _, err := os.Stat(filepath.Join(root, "cue.mod", "usr")); !os.IsNotExist(err) {
		t.Fatal("sync should not create a vendored copy when none exists")
	}
}

// TestVendorSyncSkipsSymlink ensures a symlinked vendored schema (the dev/
// example layout pointing at live source) is never clobbered by sync.
func TestVendorSyncSkipsSymlink(t *testing.T) {
	root := t.TempDir()
	// A separate, intentionally-divergent source the symlink points at.
	src := t.TempDir()
	if err := writeSchemaTo(src); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "types.cue"), []byte("package creidhne // live edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vendorDir := filepath.Join(root, "cue.mod", "usr", filepath.FromSlash(eval.ModulePath))
	if err := os.MkdirAll(filepath.Dir(vendorDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, vendorDir); err != nil {
		t.Fatal(err)
	}

	syncVendoredSchema(root) // must be a no-op on a symlink

	fi, err := os.Lstat(vendorDir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("sync replaced the symlinked vendored schema with a real copy")
	}
}

// TestVendorSyncRemovesStaleExtraFile: a vendored file no longer present in the
// embedded schema is treated as drift and cleaned up by sync.
func TestVendorSyncRemovesStaleExtraFile(t *testing.T) {
	root := t.TempDir()
	if err := vendorSchema(root); err != nil {
		t.Fatal(err)
	}
	vendorDir := filepath.Join(root, "cue.mod", "usr", filepath.FromSlash(eval.ModulePath))
	stale := filepath.Join(vendorDir, "removed_in_newer_binary.cue")
	if err := os.WriteFile(stale, []byte("package creidhne\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if vendoredMatchesEmbedded(vendorDir) {
		t.Fatal("a stale extra file should count as drift")
	}
	syncVendoredSchema(root)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("sync should have removed the stale extra file")
	}
	if !vendoredMatchesEmbedded(vendorDir) {
		t.Fatal("after sync the vendored copy should match the embedded schema")
	}
}

// TestNormalizeVendorSource: credentials never reach the recorded source —
// including userinfo git accepts but net/url rejects — and local paths become
// absolute so a restore doesn't depend on the caller's working directory.
func TestNormalizeVendorSource(t *testing.T) {
	cases := map[string]struct{ clone, record string }{
		"https://token@example.com/mod.git":        {"https://token@example.com/mod.git", "https://example.com/mod.git"},
		"https://user:p%|[ss@example.com/mod.git":  {"https://user:p%|[ss@example.com/mod.git", "https://example.com/mod.git"},
		"https://example.com/mod.git":              {"https://example.com/mod.git", "https://example.com/mod.git"},
		"git@example.com:org/mod.git":              {"git@example.com:org/mod.git", "git@example.com:org/mod.git"},
		"ssh://git@example.com/org/mod.git":        {"ssh://git@example.com/org/mod.git", "ssh://git@example.com/org/mod.git"},
	}
	for in, want := range cases {
		clone, record, _ := normalizeVendorSource(in)
		if clone != want.clone || record != want.record {
			t.Errorf("normalizeVendorSource(%q) = (%q, %q), want (%q, %q)", in, clone, record, want.clone, want.record)
		}
	}

	// A local path that exists resolves to absolute for both clone and lock.
	dir := t.TempDir()
	t.Chdir(filepath.Dir(dir))
	clone, record, _ := normalizeVendorSource(filepath.Base(dir))
	if clone != dir || record != dir {
		t.Errorf("local source: got (%q, %q), want both %q", clone, record, dir)
	}
}
