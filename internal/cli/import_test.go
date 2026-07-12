package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const importCompose = `name: web
services:
  nginx:
    image: docker.io/library/nginx:1.27
    ports: ["8080:80"]
`

func TestCmdImportCompose(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(importCompose), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	out, err := runCmd(t, "import", "compose", "compose.yaml")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "wrote ") {
		t.Fatalf("expected wrote message:\n%s", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "web.cue"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"web: creidhne.#Quadlet", "#container:", `"8080:80",`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("emitted CUE missing %q:\n%s", want, raw)
		}
	}

	// Refuses to overwrite without --force.
	if _, err := runCmd(t, "import", "compose", "compose.yaml"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected overwrite refusal, got: %v", err)
	}
	if out, err := runCmd(t, "import", "compose", "compose.yaml", "--force"); err != nil {
		t.Fatalf("--force should overwrite: %v\n%s", err, out)
	}
}

func TestCmdImportComposeStdoutAndName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(importCompose), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out, err := runCmd(t, "import", "compose", "compose.yaml", "-o", "-", "--name", "frontend")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "frontend: creidhne.#Quadlet") || !strings.Contains(out, `name: "frontend"`) {
		t.Fatalf("stdout emission with --name wrong:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "frontend.cue")); err == nil {
		t.Fatal("-o - must not write a file")
	}
}

// TestCmdImportComposeEnvResolve: --env-file bakes values instead of lifting.
func TestCmdImportComposeEnvResolve(t *testing.T) {
	dir := t.TempDir()
	compose := `name: envy
services:
  app:
    image: "docker.io/acme/app:${APP_VERSION}"
`
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.env"), []byte("APP_VERSION=9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	// Default: lifted env var.
	out, err := runCmd(t, "import", "compose", "compose.yaml", "-o", "-")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "APP_VERSION: string") || !strings.Contains(out, `\(env.APP_VERSION)`) {
		t.Fatalf("expected lifted env var:\n%s", out)
	}

	// Baked with --env-file.
	out, err = runCmd(t, "import", "compose", "compose.yaml", "-o", "-", "--env-file", "app.env")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, `Image: "docker.io/acme/app:9.9"`) || strings.Contains(out, "env:") {
		t.Fatalf("expected baked value with no env struct:\n%s", out)
	}
}
