package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitModuleRepo builds a local git repo holding a CUE helper module.
func gitModuleRepo(t *testing.T, modulePath string, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	mustWrite(t, filepath.Join(repo, "cue.mod", "module.cue"),
		"module: \""+modulePath+"@v0\"\nlanguage: version: \"v0.16.0\"\n")
	for rel, content := range files {
		mustWrite(t, filepath.Join(repo, rel), content)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "init"},
		{"tag", "v0.1.0"},
	} {
		if out, err := runGit(repo, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

const helperModule = "example.com/helpers"

const helperCUE = `package helpers

import (
	"strings"

	creidhne "github.com/lugoues/creidhne@v0"
)

// #WebLabels computes labels; imports the schema and stdlib only.
#WebLabels: {
	app!: string
	_v:   creidhne.#KeyValue & "app=\(app)"
	out: [_v, "tier=" + strings.ToLower("WEB")]
}
`

func TestVendorAndUseModule(t *testing.T) {
	repo := gitModuleRepo(t, helperModule, map[string]string{"helpers.cue": helperCUE})
	proj := setupProject(t, `package quadlets
import (
	"github.com/lugoues/creidhne@v0"
	"example.com/helpers@v0"
)
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: Container: {
		Image: "docker.io/x"
		Label: [(helpers.#WebLabels & {app: "web"}).out]
	}
}
`)

	out, err := runCmd(t, "--dir", proj, "vendor", helperModule+"@v0.1.0", "--source", repo)
	if err != nil {
		t.Fatalf("vendor: %v\n%s", err, out)
	}
	if !strings.Contains(out, "vendored example.com/helpers@v0.1.0") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(proj, "cue.mod", "usr", "example.com", "helpers", "helpers.cue")); err != nil {
		t.Fatalf("vendored file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "cue.mod", vendorLockName)); err != nil {
		t.Fatalf("lock missing: %v", err)
	}

	// The vendored module resolves offline through cue.mod/usr, composes with
	// the embedded schema, and flattens through the Label list.
	out, err = runCmd(t, "--dir", proj, "render")
	if err != nil {
		t.Fatalf("render with vendored module: %v\n%s", err, out)
	}
	for _, want := range []string{"Label=app=web", "Label=tier=web"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}

	// --check: clean, then drift, then restore.
	if out, err := runCmd(t, "--dir", proj, "vendor", "--check"); err != nil || !strings.Contains(out, "ok (v0.1.0@") {
		t.Fatalf("check should pass: %v\n%s", err, out)
	}
	vendored := filepath.Join(proj, "cue.mod", "usr", "example.com", "helpers", "helpers.cue")
	if err := os.WriteFile(vendored, []byte("package helpers\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runCmd(t, "--dir", proj, "vendor", "--check"); err == nil || !strings.Contains(out, "drifted") {
		t.Fatalf("check should detect drift: %v\n%s", err, out)
	}
	if out, err := runCmd(t, "--dir", proj, "vendor", helperModule+"@v0.1.0", "--source", repo); err != nil {
		t.Fatalf("re-vendor: %v\n%s", err, out)
	}
	if out, err := runCmd(t, "--dir", proj, "vendor", "--check"); err != nil {
		t.Fatalf("check after restore: %v\n%s", err, out)
	}
}

func TestVendorRefusesTransitiveImports(t *testing.T) {
	repo := gitModuleRepo(t, helperModule, map[string]string{"bad.cue": `package helpers

import "github.com/somebody/else@v1"

x: else.Thing
`})
	proj := setupProject(t, "package quadlets\n")
	_, err := runCmd(t, "--dir", proj, "vendor", helperModule, "--source", repo)
	if err == nil || !strings.Contains(err.Error(), "transitive module dependencies") {
		t.Fatalf("expected transitive-import refusal, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(proj, "cue.mod", "usr", "example.com")); !os.IsNotExist(statErr) {
		t.Fatal("nothing should have been installed on refusal")
	}
}

func TestVendorRefusesModuleMismatch(t *testing.T) {
	repo := gitModuleRepo(t, "example.com/other", map[string]string{"a.cue": "package other\n"})
	proj := setupProject(t, "package quadlets\n")
	_, err := runCmd(t, "--dir", proj, "vendor", helperModule, "--source", repo)
	if err == nil || !strings.Contains(err.Error(), "declares module") {
		t.Fatalf("expected module mismatch error, got: %v", err)
	}
}
