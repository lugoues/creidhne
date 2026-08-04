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
		"module: \""+modulePath+"@v0\"\nlanguage: version: \"v0.17.0\"\n")
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
	if err == nil || !strings.Contains(err.Error(), "vendored first") {
		t.Fatalf("expected unvendored-import refusal, got: %v", err)
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

// TestVendorRejectsTraversalModule: a module argument like ../../victim,
// paired with a repo declaring that same string, must be rejected before the
// unconditional RemoveAll under cue.mod/usr can reach outside it and destroy
// (then squat) an arbitrary project path. (REVIEW-1 finding 3)
func TestVendorRejectsTraversalModule(t *testing.T) {
	repo := gitModuleRepo(t, "../../victim", map[string]string{
		"payload.cue": "package victim\n\nx: 1\n",
	})
	proj := setupProject(t, testMain)
	sentinel := filepath.Join(proj, "victim", "keep.txt")
	mustWrite(t, sentinel, "precious")

	_, err := runCmd(t, "--dir", proj, "vendor", "../../victim", "--source", repo)
	if err == nil {
		t.Fatal("a traversal module path must be rejected")
	}
	if _, statErr := os.Stat(sentinel); statErr != nil {
		t.Fatalf("the traversal destroyed data outside cue.mod/usr: %v", statErr)
	}
}

// TestVendorRejectsSymlinkedDest: a symlinked component under cue.mod/usr
// must not redirect the wholesale RemoveAll+install outside it.
func TestVendorRejectsSymlinkedDest(t *testing.T) {
	repo := gitModuleRepo(t, "example.com/helpers", map[string]string{
		"payload.cue": "package helpers\n\nx: 1\n",
	})
	proj := setupProject(t, testMain)
	outside := filepath.Join(t.TempDir(), "elsewhere")
	sentinel := filepath.Join(outside, "helpers", "keep.txt")
	mustWrite(t, sentinel, "precious")
	usr := filepath.Join(proj, "cue.mod", "usr")
	if err := os.MkdirAll(usr, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(usr, "example.com")); err != nil {
		t.Fatal(err)
	}

	_, err := runCmd(t, "--dir", proj, "vendor", "example.com/helpers", "--source", repo)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("want a symlink refusal, got %v", err)
	}
	if _, statErr := os.Stat(sentinel); statErr != nil {
		t.Fatalf("install went through the symlink: %v", statErr)
	}
}

// TestVendorRejectsSymlinkedUsrRoot: the containment root itself
// (cue.mod/usr) being a symlink must be rejected too — Rel-based containment
// is lexical and passes, but RemoveAll/install would land wherever the link
// points. (Sol re-review of finding 3)
func TestVendorRejectsSymlinkedUsrRoot(t *testing.T) {
	repo := gitModuleRepo(t, "example.com/helpers", map[string]string{
		"payload.cue": "package helpers\n\nx: 1\n",
	})
	proj := setupProject(t, testMain)
	outside := filepath.Join(t.TempDir(), "elsewhere")
	sentinel := filepath.Join(outside, "example.com", "helpers", "keep.txt")
	mustWrite(t, sentinel, "precious")
	if err := os.Symlink(outside, filepath.Join(proj, "cue.mod", "usr")); err != nil {
		t.Fatal(err)
	}

	_, err := runCmd(t, "--dir", proj, "vendor", "example.com/helpers", "--source", repo)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("want a symlink refusal for the usr root, got %v", err)
	}
	if _, statErr := os.Stat(sentinel); statErr != nil {
		t.Fatalf("install went through the symlinked usr root: %v", statErr)
	}
}

// TestVendorAllowsVendoredDependency: a module may import another module once
// that module is vendored in the project (present in the lock). Order is
// explicit: the dependent is refused with a hint until its dependency is
// vendored; crei never fetches transitively.
func TestVendorAllowsVendoredDependency(t *testing.T) {
	depRepo := gitModuleRepo(t, "example.com/base", map[string]string{
		"base.cue": "package base\n\nAnswer: 42\n",
	})
	appRepo := gitModuleRepo(t, "example.com/stacks", map[string]string{
		"stack.cue": `package stacks

import "example.com/base@v0"

x: base.Answer
`})
	proj := setupProject(t, "package quadlets\n")

	// Dependent first: refused, naming the missing dependency.
	_, err := runCmd(t, "--dir", proj, "vendor", "example.com/stacks", "--source", appRepo)
	if err == nil || !strings.Contains(err.Error(), "vendored first") || !strings.Contains(err.Error(), "example.com/base") {
		t.Fatalf("want a vendored-first refusal naming example.com/base, got: %v", err)
	}

	// Dependency, then dependent: both succeed.
	if _, err := runCmd(t, "--dir", proj, "vendor", "example.com/base", "--source", depRepo); err != nil {
		t.Fatalf("vendor dependency: %v", err)
	}
	if _, err := runCmd(t, "--dir", proj, "vendor", "example.com/stacks", "--source", appRepo); err != nil {
		t.Fatalf("vendor dependent after its dependency: %v", err)
	}
	for _, p := range []string{
		filepath.Join("usr", "example.com", "base", "base.cue"),
		filepath.Join("usr", "example.com", "stacks", "stack.cue"),
	} {
		if _, err := os.Stat(filepath.Join(proj, "cue.mod", p)); err != nil {
			t.Fatalf("expected vendored file %s: %v", p, err)
		}
	}
}
