package cli

import (
	"bytes"
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
		"module: \"example.com/test@v0\"\nlanguage: version: \"v0.17.0\"\n")
	mustWrite(t, filepath.Join(dir, "main.cue"), mainCue)
	return dir
}

// runCmd executes the root command with args, capturing its output via cobra's
// SetOut/SetErr (no global os.Stdout mutation, so these tests are isolated and
// could run in parallel). Stdin is an empty reader, so a command that prompts
// for confirmation sees EOF unless given -y.
func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	root := newRootCmd()
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestCmdInit(t *testing.T) {
	dir := t.TempDir()
	if _, err := runCmd(t, "--dir", dir, "init"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"cue.mod/module.cue", "main.cue", ".crei/config.toml", ".crei/config.schema.json", "cue.mod/usr"} {
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

const twoQuadletMain = `package config
import q "github.com/lugoues/creidhne@v0"
web: q.#Quadlet & {
	name: "web"
	units: #container: {Container: {Image: "docker.io/nginx:latest", ContainerName: "web"}}
}
db: q.#Quadlet & {
	name: "db"
	units: #container: {Container: {Image: "docker.io/postgres:16", ContainerName: "db"}}
}
`

func TestCmdRenderByQuadletName(t *testing.T) {
	dir := setupProject(t, twoQuadletMain)

	// No args renders the whole stack.
	out, err := runCmd(t, "--dir", dir, "render")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "web.container") || !strings.Contains(out, "db.container") {
		t.Fatalf("expected both quadlets:\n%s", out)
	}

	// A named quadlet renders only its files.
	out, err = runCmd(t, "--dir", dir, "render", "web")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "web.container") || strings.Contains(out, "db.container") {
		t.Fatalf("expected only web:\n%s", out)
	}

	// Multiple names render just those.
	out, err = runCmd(t, "--dir", dir, "render", "db", "web")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "web.container") || !strings.Contains(out, "db.container") {
		t.Fatalf("expected both named:\n%s", out)
	}

	// An unknown name fails loudly and lists what is available.
	_, err = runCmd(t, "--dir", dir, "render", "nope")
	if err == nil || !strings.Contains(err.Error(), "available") {
		t.Fatalf("expected an error listing available quadlets, got: %v", err)
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

// buildAbsContextMain is a project whose build context uses an absolute-path
// key, the shape that used to make apply oscillate add<->remove.
const buildAbsContextMain = `package config
import q "github.com/lugoues/creidhne@v0"
hermes: q.#Quadlet & {
	name: "hermes"
	units: #build: {
		ContainerFile: "FROM scratch\n"
		Context: "/home/hermes/.local/bin/hermes-gateways": {
			mode:    "0770"
			content: "#!/bin/sh\necho hi\n"
		}
	}
}
`

// TestCmdContextAbsoluteKeyIsIdempotent guards the reported bug: a build context
// keyed by an absolute path oscillated add<->remove on every apply (and deleted
// the context file every other run). After the first apply the plan must be a
// no-op and the file must remain.
func TestCmdContextAbsoluteKeyIsIdempotent(t *testing.T) {
	dir := setupProject(t, buildAbsContextMain)
	qd := t.TempDir()
	if _, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "apply", "-y", "--reload-systemd=false"); err != nil {
		t.Fatal(err)
	}
	out, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Nothing to do") {
		t.Fatalf("expected an idempotent plan, got:\n%s", out)
	}
	ctxFile := filepath.Join(qd, "images", "hermes.context", "home", "hermes", ".local", "bin", "hermes-gateways")
	if _, err := os.Stat(ctxFile); err != nil {
		t.Fatalf("context file missing after apply: %v", err)
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

// TestCmdDiffStyleInline: diff_style="inline" renders a modified line as a single
// "~" line carrying both the removed and added runs, not the "- old" / "+ new"
// pair. Stripped of color, the line holds the common prefix plus both values.
func TestCmdDiffStyleInline(t *testing.T) {
	dir := setupProject(t, testMain)
	mustWrite(t, filepath.Join(dir, ".crei", "config.toml"), "diff_style = \"inline\"\n")
	qd := t.TempDir()
	if _, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "apply", "-y", "--reload-systemd=false"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "main.cue"), strings.Replace(testMain, "nginx:latest", "nginx:1.0", 1))
	out, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "diff")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "~ Image=docker.io/nginx:") {
		t.Errorf("inline style should render the change as one '~' line:\n%s", out)
	}
	// Both the removed and added runs are present (word-diff), and there is no
	// "- old" / "+ new" pair.
	if !strings.Contains(out, "latest") || !strings.Contains(out, "1.0") {
		t.Errorf("inline style should show both old and new runs:\n%s", out)
	}
	if strings.Contains(out, "- Image=") || strings.Contains(out, "+ Image=") {
		t.Errorf("inline style should not emit the '- old' / '+ new' pair:\n%s", out)
	}
}

// TestCmdCrossQuadletNetworkName is a regression test: a quadlet that references
// another quadlet's computed #networkName renders it blank under a specific (and
// realistic) combination — the reference goes through a *named* unit (the plural
// `containers` map), and the two quadlets are defined in *separate files*. The
// #<type>Name fields used to be defined conditionally (`if #unitType == "..."`);
// that conditional definition resolved to "" when read cross-file through the
// manifest comprehension, so the consumer's rendered label silently blanked.
// Both factors are load-bearing: the same content in one file, or via the
// #container primary, resolved fine — which is why it was so hard to pin down.
// Fixed by defining the name fields unconditionally in creidhne/reference.cue.
func TestCmdCrossQuadletNetworkName(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "cue.mod", "module.cue"),
		"module: \"example.com/test@v0\"\nlanguage: version: \"v0.17.0\"\n")
	mustWrite(t, filepath.Join(dir, "provider.cue"), `package config
import q "github.com/lugoues/creidhne@v0"
provider: q.#Quadlet & {name: "provider", units: networks: internal: Network: Internal: true}
`)
	// Separate file + named container ("web") are what trigger the old bug.
	mustWrite(t, filepath.Join(dir, "consumer.cue"), `package config
import q "github.com/lugoues/creidhne@v0"
consumer: q.#Quadlet & {name: "consumer", units: containers: "web": Container: {
	Image: "img"
	Label: ["net=\(provider.units.networks.internal.#networkName)"]
}}
`)
	out, err := runCmd(t, "--dir", dir, "render")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "net=systemd-provider-internal") {
		t.Errorf("cross-quadlet #networkName must resolve, got blank (regression):\n%s", out)
	}
}

// --- adversarial / negative command-level cases ---

func TestCmdRenderInvalidCUE(t *testing.T) {
	bad := setupProject(t, `package config
import q "github.com/lugoues/creidhne@v0"
z: q.#Quadlet & {name: "z", units: #container: Container: {Image: "img", ContainerName: "x", AutoUpdate: "bogus"}}
`)
	if _, err := runCmd(t, "--dir", bad, "render"); err == nil {
		t.Fatal("expected error rendering an invalid enum value")
	}
}

func TestCmdNoQuadlets(t *testing.T) {
	none := setupProject(t, "package config\n\nx: 1\n")
	_, err := runCmd(t, "--dir", none, "render")
	if err == nil || !strings.Contains(err.Error(), "no quadlets") {
		t.Fatalf("expected 'no quadlets' error, got %v", err)
	}
}

func TestCmdMissingModule(t *testing.T) {
	dir := t.TempDir() // no cue.mod anywhere up the tree
	mustWrite(t, filepath.Join(dir, "main.cue"), testMain)
	if _, err := runCmd(t, "--dir", dir, "render"); err == nil {
		t.Fatal("expected error when no cue.mod is found")
	}
}

func TestCmdDuplicateFilenameError(t *testing.T) {
	dup := setupProject(t, `package config
import q "github.com/lugoues/creidhne@v0"
a: q.#Quadlet & {name: "app-web", units: #container: Container: {Image: "A", ContainerName: "x"}}
b: q.#Quadlet & {name: "app", units: containers: web: Container: {Image: "B", ContainerName: "y"}}
`)
	_, err := runCmd(t, "--dir", dup, "render")
	if err == nil || !strings.Contains(err.Error(), "duplicate output file") {
		t.Fatalf("expected duplicate-filename error, got %v", err)
	}
}

func TestCmdApplyRequiresConfirmation(t *testing.T) {
	dir := setupProject(t, testMain)
	qd := t.TempDir()
	// runCmd feeds an empty stdin, so apply without -y sees EOF (no answer).
	_, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "apply") // no -y
	if err == nil {
		t.Fatal("apply without -y and no confirmation should error, not silently abort")
	}
	if _, statErr := os.Stat(filepath.Join(qd, "z.container")); statErr == nil {
		t.Fatal("apply must not have written files when it errored on confirmation")
	}
}

func TestCmdPlanShowsRemoval(t *testing.T) {
	two := `package config
import q "github.com/lugoues/creidhne@v0"
a: q.#Quadlet & {name: "a", units: #container: Container: {Image: "img", ContainerName: "a"}}
b: q.#Quadlet & {name: "b", units: #container: Container: {Image: "img", ContainerName: "b"}}
`
	dir := setupProject(t, two)
	qd := t.TempDir()
	if _, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "apply", "-y", "--reload-systemd=false"); err != nil {
		t.Fatal(err)
	}
	// Drop quadlet b; its file is now stale.
	mustWrite(t, filepath.Join(dir, "main.cue"), `package config
import q "github.com/lugoues/creidhne@v0"
a: q.#Quadlet & {name: "a", units: #container: Container: {Image: "img", ContainerName: "a"}}
`)
	out, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "b.container") || !strings.Contains(out, "1 to remove") {
		t.Fatalf("plan should schedule b.container for removal:\n%s", out)
	}
}

// TestCmdPlanShowsDiff: plan renders the inline diff by default (the new file's
// content as added lines), not just the change list.
func TestCmdPlanShowsDiff(t *testing.T) {
	dir := setupProject(t, testMain)
	qd := t.TempDir()
	out, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+ Image=docker.io/nginx:latest") {
		t.Errorf("plan should show the inline diff body:\n%s", out)
	}
}

// TestCmdPlanNoDiff: --no-diff shows the compact change list, not file content.
func TestCmdPlanNoDiff(t *testing.T) {
	dir := setupProject(t, testMain)
	qd := t.TempDir()
	out, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "plan", "--no-diff")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+ z.container") {
		t.Errorf("--no-diff should show the change list:\n%s", out)
	}
	if strings.Contains(out, "Image=docker.io/nginx") {
		t.Errorf("--no-diff should not show file content:\n%s", out)
	}
}

func TestCmdApplyPermissionHint(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses filesystem permissions")
	}
	dir := setupProject(t, testMain)
	qd := t.TempDir()
	if err := os.Chmod(qd, 0o555); err != nil { // read-only dir
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(qd, 0o755) }() // restore so TempDir cleanup works
	_, err := runCmd(t, "--dir", dir, "--quadlet-dir", qd, "apply", "-y", "--reload-systemd=false")
	if err == nil {
		t.Fatal("expected a permission error writing to a read-only quadlet dir")
	}
	if !strings.Contains(err.Error(), "sudo") {
		t.Errorf("permission error should hint at sudo, got: %v", err)
	}
}
