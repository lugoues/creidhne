package creidhne_test

import (
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lugoues/creidhne"
	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/render"
)

// update regenerates each fixture's expected/ tree from the current renderer:
// `go test . -run TestGolden -update`. It only writes files that are new or
// changed, so unchanged (and possibly read-only) fixtures are left untouched.
var update = flag.Bool("update", false, "rewrite golden expected/ files")

// TestGolden is the module's end-to-end test: it renders every testdata/<case>
// fixture through the *embedded* templates and schema (the schema resolves via
// an overlay) and the eval+render packages, asserting byte-equality against the
// case's expected/ tree. It lives at the repo root (next to the fixtures) so it
// reaches them without relative-path traversal.
func TestGolden(t *testing.T) {
	tplFS, err := fs.Sub(creidhne.TemplatesFS, "templates")
	if err != nil {
		t.Fatalf("templates sub-fs: %v", err)
	}
	r, err := render.New(tplFS)
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}

	const root = "testdata"
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	// Resolve the schema import (github.com/lugoues/creidhne) from the embedded
	// SchemaFS via an overlay. The harness import (quadlets-test:testing) still
	// resolves from the on-disk testdata module.
	overlay, err := eval.Overlay(rootAbs, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		// "invalid" holds negative fixtures handled by TestGoldenNegative.
		if !e.IsDir() || e.Name() == "cue.mod" || e.Name() == "invalid" {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			caseDir := filepath.Join(root, name)
			quads, err := eval.LoadAndValidate(caseDir, overlay)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			got, err := r.BuildFileSet(quads)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if *update {
				writeGolden(t, filepath.Join(caseDir, "expected"), got)
				return
			}
			want := readExpected(t, filepath.Join(caseDir, "expected"))

			if g, w := keys(got), keysBytes(want); strings.Join(g, ",") != strings.Join(w, ",") {
				t.Fatalf("file set mismatch:\n got:  %v\n want: %v", g, w)
			}
			for k, w := range want {
				if string(got[k].Content) != string(w) {
					t.Errorf("%s differs:\n--- got ---\n%q\n--- want ---\n%q", k, got[k].Content, w)
				}
			}
		})
	}
}

// writeGolden writes the rendered file set under dir, creating it as needed and
// skipping files whose content is already identical (so unchanged, possibly
// read-only fixtures are not rewritten).
func writeGolden(t *testing.T, dir string, got map[string]render.FileContent) {
	t.Helper()
	for name, fc := range got {
		dest := filepath.Join(dir, filepath.FromSlash(name))
		if existing, err := os.ReadFile(dest); err == nil && string(existing) == string(fc.Content) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, fc.Content, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", dest)
	}
}

// TestGoldenNegative checks that each testdata/invalid/<case> fails to load,
// with an error containing the substring in its want_error file. Substrings
// (not exact text) keep these robust across CUE/Go versions.
func TestGoldenNegative(t *testing.T) {
	const root = "testdata"
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := eval.Overlay(rootAbs, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "invalid")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("no negative fixtures")
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			caseDir := filepath.Join(dir, name)
			wantBytes, err := os.ReadFile(filepath.Join(caseDir, "want_error"))
			if err != nil {
				t.Fatalf("read want_error: %v", err)
			}
			want := strings.TrimSpace(string(wantBytes))
			if _, err := eval.LoadAndValidate(caseDir, overlay); err == nil {
				t.Fatalf("expected load to fail with an error containing %q, got nil", want)
			} else if want != "" && !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q does not contain %q", err.Error(), want)
			}
		})
	}
}

func readExpected(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func keys(m map[string]render.FileContent) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}

func keysBytes(m map[string][]byte) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}
