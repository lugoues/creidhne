package importer

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
)

func TestRewriteRawURL(t *testing.T) {
	cases := map[string]string{
		// GitHub blob pages rewrite to raw content.
		"https://github.com/docker/awesome-compose/blob/master/nginx-golang/compose.yaml": "https://raw.githubusercontent.com/docker/awesome-compose/master/nginx-golang/compose.yaml",
		// Raw URLs pass through.
		"https://raw.githubusercontent.com/u/r/main/compose.yaml": "https://raw.githubusercontent.com/u/r/main/compose.yaml",
		// GitLab blob pages rewrite to raw.
		"https://gitlab.com/group/proj/-/blob/main/compose.yaml": "https://gitlab.com/group/proj/-/raw/main/compose.yaml",
		// Anything else passes through.
		"https://example.com/stacks/compose.yaml": "https://example.com/stacks/compose.yaml",
	}
	for in, want := range cases {
		if got := rewriteRawURL(in); got != want {
			t.Errorf("rewriteRawURL(%q)\n got  %q\n want %q", in, got, want)
		}
	}
}

func TestDeriveProjectName(t *testing.T) {
	cases := map[string]string{
		// GitHub: directory containing the file, else the repo.
		"https://github.com/docker/awesome-compose/blob/master/nginx-golang/compose.yaml": "nginx-golang",
		"https://github.com/user/myapp/blob/main/compose.yaml":                            "myapp",
		"https://raw.githubusercontent.com/user/myapp/main/compose.yaml":                  "myapp",
		"https://raw.githubusercontent.com/user/myapp/main/sub/dir/compose.yaml":          "dir",
		// GitLab: in-repo path after the ref, else the project (never the ref).
		"https://gitlab.com/group/proj/-/blob/main/compose.yaml":       "proj",
		"https://gitlab.com/group/proj/-/blob/main/stack/compose.yaml": "stack",
		// Generic: last directory segment; nothing usable -> "".
		"https://example.com/stacks/webapp/compose.yaml": "webapp",
		"https://example.com/compose.yaml":               "",
		// Normalization into compose's name alphabet.
		"https://github.com/user/My.App/blob/main/compose.yaml": "my-app",
	}
	for in, want := range cases {
		if got := deriveProjectName(in); got != want {
			t.Errorf("deriveProjectName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConvertRemote(t *testing.T) {
	const compose = `services:
  nginx:
    image: docker.io/library/nginx:1.27
    ports: ["8080:80"]
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stacks/demo/compose.yaml":
			_, _ = w.Write([]byte(compose))
		case "/page":
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body>a forge page</body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	res, err := Convert(Options{Paths: []string{srv.URL + "/stacks/demo/compose.yaml"}})
	if err != nil {
		t.Fatal(err)
	}
	// Name derives from the URL directory; the compose has no name: field.
	if res.QuadletName != "demo" {
		t.Fatalf("QuadletName = %q, want demo", res.QuadletName)
	}
	if !strings.Contains(string(res.CUE), "docker.io/library/nginx:1.27") {
		t.Fatalf("emitted CUE missing image:\n%s", res.CUE)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "fetched from a URL") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the relative-paths warning, got %v", res.Warnings)
	}

	// --name wins over derivation.
	res, err = Convert(Options{Paths: []string{srv.URL + "/stacks/demo/compose.yaml"}, ProjectName: "frontend"})
	if err != nil {
		t.Fatal(err)
	}
	if res.QuadletName != "frontend" {
		t.Fatalf("QuadletName = %q, want frontend", res.QuadletName)
	}

	// An HTML page (a forge blob page that slipped through) is rejected.
	if _, err := Convert(Options{Paths: []string{srv.URL + "/page"}, ProjectName: "x"}); err == nil || !strings.Contains(err.Error(), "raw file URL") {
		t.Fatalf("expected HTML rejection, got: %v", err)
	}

	// A 404 is a clear error.
	if _, err := Convert(Options{Paths: []string{srv.URL + "/missing.yaml"}, ProjectName: "x"}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected HTTP error, got: %v", err)
	}
}

// TestStructuredVarsHint: a ${VAR} inside a structured field fails the
// symbolic (default) load with guidance toward the resolve modes; the same
// file imports fine with ResolveEnv.
func TestStructuredVarsHint(t *testing.T) {
	dir := t.TempDir()
	compose := "name: hinty\nservices:\n  app:\n    image: docker.io/x\n    ports:\n      - \"${PORT:-8080}:80\"\n"
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Convert(Options{Paths: []string{filepath.Join(dir, "compose.yaml")}, WorkingDir: dir})
	if err == nil || !strings.Contains(err.Error(), "--resolve") {
		t.Fatalf("expected the resolve hint, got: %v", err)
	}
	res, err := Convert(Options{Paths: []string{filepath.Join(dir, "compose.yaml")}, WorkingDir: dir, ResolveEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.CUE), `"8080:80",`) {
		t.Fatalf("resolve mode should bake the default port:\n%s", res.CUE)
	}
}

// TestRemoteEnvFileAndPositionalEnvHint: --env-file accepts URLs, and an env
// file passed positionally gets a pointed error instead of a yaml misparse.
func TestRemoteEnvFileAndPositionalEnvHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stack/compose.yaml":
			_, _ = w.Write([]byte("services:\n  app:\n    image: \"docker.io/acme/app:${APP_VERSION}\"\n"))
		case "/stack/app.env":
			_, _ = w.Write([]byte("# paperless-style env file\nAPP_VERSION=7.7\nOTHER=x\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Remote env file resolves values.
	res, err := Convert(Options{
		Paths:      []string{srv.URL + "/stack/compose.yaml"},
		EnvFiles:   []string{srv.URL + "/stack/app.env"},
		ResolveEnv: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.CUE), `"docker.io/acme/app:7.7"`) {
		t.Fatalf("remote env file value not applied:\n%s", res.CUE)
	}

	// The same env file passed positionally errors with guidance.
	_, err = Convert(Options{
		Paths:      []string{srv.URL + "/stack/compose.yaml", srv.URL + "/stack/app.env"},
		ResolveEnv: true,
	})
	if err == nil || !strings.Contains(err.Error(), "--env-file") {
		t.Fatalf("expected env-file guidance, got: %v", err)
	}
}

// TestNamePreservation: fresh systemd-* names by default (with a migration
// note); --preserve-names adopts the compose-era names; externals are always
// adopted by name.
func TestNamePreservation(t *testing.T) {
	dir := t.TempDir()
	compose := `name: fresh
services:
  a:
    image: docker.io/x
    volumes: [data:/d]
  b:
    image: docker.io/y
volumes:
  data:
  ext:
    external: true
    name: prod-data
`
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Convert(Options{Paths: []string{filepath.Join(dir, "compose.yaml")}, WorkingDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	cue := string(res.CUE)
	if strings.Contains(cue, `VolumeName: "fresh_data"`) {
		t.Fatalf("default must not preserve project volume names:\n%s", cue)
	}
	if !strings.Contains(cue, `VolumeName: "prod-data"`) {
		t.Fatalf("external volumes must still be adopted by name:\n%s", cue)
	}
	var noted bool
	for _, n := range res.Notes {
		if strings.Contains(n, "--preserve-names") && strings.Contains(n, "fresh_data") {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("expected the migration note, got %v", res.Notes)
	}

	res, err = Convert(Options{Paths: []string{filepath.Join(dir, "compose.yaml")}, WorkingDir: dir, PreserveNames: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.CUE), `VolumeName: "fresh_data"`) {
		t.Fatalf("--preserve-names must adopt project volume names:\n%s", res.CUE)
	}
}

// TestConvertRemoteNameFromFile: a name: field in the fetched compose wins
// over URL derivation.
func TestConvertRemoteNameFromFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("name: fromfile\nservices:\n  app:\n    image: docker.io/x\n"))
	}))
	defer srv.Close()
	res, err := Convert(Options{Paths: []string{srv.URL + "/stacks/demo/compose.yaml"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.QuadletName != "fromfile" {
		t.Fatalf("QuadletName = %q, want fromfile (name: field wins)", res.QuadletName)
	}
}

// TestEmbedSource: sources embed as trailing comments labeled by URL; inline
// secret content withholds the embed; OmitSource disables it.
func TestEmbedSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stack/compose.yaml":
			_, _ = w.Write([]byte("# instructional comment\nservices:\n  app:\n    image: docker.io/x\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	res, err := Convert(Options{Paths: []string{srv.URL + "/stack/compose.yaml"}, ProjectName: "embed"})
	if err != nil {
		t.Fatal(err)
	}
	cue := string(res.CUE)
	if !strings.Contains(cue, "// ─── source: "+srv.URL+"/stack/compose.yaml") {
		t.Fatalf("embed header missing or not URL-labeled:\n%s", cue)
	}
	if !strings.Contains(cue, "// # instructional comment") {
		t.Fatalf("source comment not embedded:\n%s", cue)
	}

	// Inline secret content withholds the embed. (compose-go's current schema
	// rejects content: at load, so exercise the guard directly; it is
	// insurance for the compose-go bump that starts accepting it.)
	dir := t.TempDir()
	src := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(src, []byte("secrets:\n  tok:\n    content: super-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := &types.Project{
		ComposeFiles: []string{src},
		Secrets:      types.Secrets{"tok": {Content: "super-secret"}},
	}
	var notes []string
	sources := collectSources(proj, Options{}, map[string]string{}, func(f string, a ...any) {
		notes = append(notes, fmt.Sprintf(f, a...))
	})
	if len(sources) != 0 {
		t.Fatalf("inline secret content must withhold the embed, got %v", sources)
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "not embedded") {
		t.Fatalf("expected a not-embedded note, got %v", notes)
	}

	// OmitSource drops the block entirely.
	res, err = Convert(Options{Paths: []string{srv.URL + "/stack/compose.yaml"}, ProjectName: "embed", OmitSource: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(res.CUE), "─── source:") {
		t.Fatalf("OmitSource must drop the embed:\n%s", res.CUE)
	}
}
