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
