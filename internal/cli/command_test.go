package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive the cobra commands end-to-end (the RunE handlers wiring
// resolveConfig -> eval -> render -> reconcile), which the package-level tests
// don't reach. The schema import resolves from the embedded overlay, so the
// temp project only needs a cue.mod and a main.cue.

const testMain = `package config

import q "github.com/lugoues/creidhne@v0"

z: q.#Quadlet & {
	name: "z"
	units: #container: {
		Container: {Image: "docker.io/nginx:latest", ContainerName: "z"}
		Install: WantedBy: ["default.target"]
	}
}
`

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupProject(t *testing.T, mainCue string) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "cue.mod", "module.cue"),
		"module: \"example.com/test@v0\"\nlanguage: version: \"v0.16.0\"\n")
	mustWrite(t, filepath.Join(dir, "main.cue"), mainCue)
	return dir
}

// runCmd executes the root command with args, capturing os.Stdout (the commands
// print there directly, not via cmd.OutOrStdout).
func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	outCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		outCh <- string(b)
	}()

	root := newRootCmd()
	root.SetArgs(args)
	runErr := root.Execute()

	_ = w.Close()
	os.Stdout = old
	out := <-outCh
	_ = r.Close()
	return out, runErr
}

func TestCmdInit(t *testing.T) {
	dir := t.TempDir()
	if _, err := runCmd(t, "--dir", dir, "init"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"cue.mod/module.cue", "main.cue", "crei.toml", "cue.mod/usr"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("init did not create %s: %v", f, err)
		}
	}
}

func TestCmdRender(t *testing.T) {
	dir := setupProject(t, testMain)
	out, err := runCmd(t, "--dir", dir, "render")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "z.container") || !strings.Contains(out, "Image=docker.io/nginx:latest") {
		t.Fatalf("unexpected render output:\n%s", out)
	}
}

func TestCmdValidate(t *testing.T) {
	dir := setupProject(t, testMain)
	out, err := runCmd(t, "--dir", dir, "validate")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "OK") {
		t.Fatalf("validate output: %q", out)
	}

	// A container missing both Image and Rootfs must fail validation.
	bad := setupProject(t, `package config
import q "github.com/lugoues/creidhne@v0"
z: q.#Quadlet & {name: "z", units: #container: Container: {ContainerName: "z"}}
`)
	if _, err := runCmd(t, "--dir", bad, "validate"); err == nil {
		t.Fatal("expected validate to fail on an incomplete container")
	}
}

func TestCmdConfig(t *testing.T) {
	dir := setupProject(t, testMain)
	out, err := runCmd(t, "--dir", dir, "--quadlet-dir", "/srv/q", "config")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "quadlet dir") || !strings.Contains(out, "/srv/q") {
		t.Fatalf("config output:\n%s", out)
	}
}

func TestCmdPlanApplyRoundtrip(t *testing.T) {
	dir := setupProject(t, testMain)
	qd := t.TempDir()

	out, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "z.container") || !strings.Contains(out, "1 to add") {
		t.Fatalf("plan output:\n%s", out)
	}

	// --reload-systemd=false avoids invoking systemctl from the test.
	out, err = runCmd(t, "--dir", dir, "--quadlet-dir", qd, "apply", "-y", "--reload-systemd=false")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Applied: 1 added") {
		t.Fatalf("apply output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(qd, "z.container")); err != nil {
		t.Fatalf("z.container was not written: %v", err)
	}

	out, err = runCmd(t, "--dir", dir, "--quadlet-dir", qd, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Nothing to do") {
		t.Fatalf("second plan should be a no-op:\n%s", out)
	}
}

func TestCmdDiff(t *testing.T) {
	dir := setupProject(t, testMain)
	qd := t.TempDir()
	if _, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "apply", "-y", "--reload-systemd=false"); err != nil {
		t.Fatal(err)
	}
	// Change the image, then diff against the live file.
	mustWrite(t, filepath.Join(dir, "main.cue"), strings.Replace(testMain, "nginx:latest", "nginx:1.0", 1))
	out, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "diff")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "changed") || !strings.Contains(out, "nginx:1.0") {
		t.Fatalf("diff output:\n%s", out)
	}
}
