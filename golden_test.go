package creidhne_test

import (
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
		if !e.IsDir() || e.Name() == "cue.mod" {
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
