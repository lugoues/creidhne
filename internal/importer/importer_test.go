package importer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue/load"

	"github.com/lugoues/creidhne"
	"github.com/lugoues/creidhne/internal/eval"
)

// TestGolden converts every testdata/<case>/compose.yaml and compares the
// emitted CUE and the conversion report byte-for-byte. Regenerate with
// UPDATE_GOLDEN=1 go test ./internal/importer/ -run TestGolden.
func TestGolden(t *testing.T) {
	cases, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	update := os.Getenv("UPDATE_GOLDEN") != ""
	for _, c := range cases {
		if !c.IsDir() {
			continue
		}
		t.Run(c.Name(), func(t *testing.T) {
			dir := filepath.Join("testdata", c.Name())
			opts := Options{
				Paths:      []string{filepath.Join(dir, "compose.yaml")},
				WorkingDir: dir,
			}
			// A "resolve" marker runs the case in bake mode (variables in
			// structured fields cannot be preserved symbolically).
			if _, err := os.Stat(filepath.Join(dir, "resolve")); err == nil {
				opts.ResolveEnv = true
			}
			res, err := Convert(opts)
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			report := renderReport(res)
			if update {
				if err := os.WriteFile(filepath.Join(dir, "expected.cue"), res.CUE, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "report.txt"), report, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			wantCUE, err := os.ReadFile(filepath.Join(dir, "expected.cue"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(res.CUE, wantCUE) {
				t.Errorf("emitted CUE differs from expected.cue:\n%s", diffHint(wantCUE, res.CUE))
			}
			wantReport, err := os.ReadFile(filepath.Join(dir, "report.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(report, wantReport) {
				t.Errorf("report differs:\n got:\n%s\n want:\n%s", report, wantReport)
			}
		})
	}
}

// TestGoldenValidates proves every emitted file type-checks against the
// embedded schema and yields a concrete manifest (the same eval pipeline
// render/apply use). Cases with lifted env vars are inherently non-concrete;
// they opt out via a skip-validate marker file.
func TestGoldenValidates(t *testing.T) {
	cases, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if !c.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join("testdata", c.Name(), "skip-validate")); err == nil {
			continue
		}
		t.Run(c.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", c.Name(), "expected.cue"))
			if err != nil {
				t.Fatal(err)
			}
			tmp := t.TempDir()
			overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
			if err != nil {
				t.Fatal(err)
			}
			overlay[filepath.Join(tmp, "cue.mod", "module.cue")] = load.FromString(
				"module: \"example.com/importtest@v0\"\nlanguage: version: \"v0.16.0\"\n")
			overlay[filepath.Join(tmp, "main.cue")] = load.FromBytes(raw)
			quads, err := eval.LoadAndValidate(tmp, overlay)
			if err != nil {
				t.Fatalf("emitted CUE does not eval: %v", err)
			}
			if len(quads) != 1 {
				t.Fatalf("want 1 quadlet, got %d", len(quads))
			}
		})
	}
}

func renderReport(res *Result) []byte {
	var b bytes.Buffer
	for _, w := range res.Warnings {
		fmt.Fprintf(&b, "! %s\n", w)
	}
	for _, n := range res.Notes {
		fmt.Fprintf(&b, "* %s\n", n)
	}
	for _, s := range res.Steps {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	return b.Bytes()
}

// diffHint shows the first differing line for quick orientation.
func diffHint(want, got []byte) string {
	w := bytes.Split(want, []byte("\n"))
	g := bytes.Split(got, []byte("\n"))
	for i := 0; i < len(w) && i < len(g); i++ {
		if !bytes.Equal(w[i], g[i]) {
			return fmt.Sprintf("line %d:\n want: %s\n got:  %s", i+1, w[i], g[i])
		}
	}
	return fmt.Sprintf("length differs: want %d lines, got %d", len(w), len(g))
}
