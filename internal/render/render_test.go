package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/kinds"
)

// TestEveryKindHasTemplate guards the shared kinds table against drift: every
// kind the renderer/reconciler manage must have a template to render it.
func TestEveryKindHasTemplate(t *testing.T) {
	for _, kind := range kinds.Kinds() {
		if _, err := os.Stat(filepath.Join("../../templates", kind+".tpl")); err != nil {
			t.Errorf("kind %q in kinds.Kinds() has no template: %v", kind, err)
		}
	}
}

// newTestRenderer loads the on-disk templates so the render tests don't depend
// on the embedded FS.
func newTestRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := New(os.DirFS("../../templates"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func containerUnit(quadlet, stem string) eval.UnitRecord {
	return eval.UnitRecord{
		Kind:     "container",
		Stem:     stem,
		Filename: stem + ".container",
		Data: map[string]any{
			"Container": map[string]any{"Image": quadlet, "ContainerName": stem},
		},
	}
}

// TestBuildFileSetDuplicateFilename ensures two units resolving to the same
// filename produce a hard error instead of a silent overwrite.
func TestBuildFileSetDuplicateFilename(t *testing.T) {
	r := newTestRenderer(t)
	quads := []eval.Quadlet{
		{Name: "app-web", Units: []eval.UnitRecord{containerUnit("img-A", "app-web")}},
		{Name: "app", Units: []eval.UnitRecord{containerUnit("img-B", "app-web")}},
	}
	_, err := r.BuildFileSet(quads)
	if err == nil {
		t.Fatal("expected duplicate-filename error, got nil")
	}
	if !strings.Contains(err.Error(), "app-web.container") {
		t.Errorf("error should name the colliding file, got: %v", err)
	}
}

// TestRenderZeroAndFalseValues ensures schema-valid falsy values (integer 0,
// boolean false) are rendered rather than silently dropped by `{{ if }}`.
func TestRenderZeroAndFalseValues(t *testing.T) {
	r := newTestRenderer(t)
	cu := eval.UnitRecord{
		Kind: "container", Stem: "z", Filename: "z.container",
		Data: map[string]any{"Container": map[string]any{
			"Image":             "img",
			"ContainerName":     "z",
			"StopTimeout":       int64(0),
			"HealthMaxLogCount": int64(0),
			"Notify":            false,
			// Explicit false must render: these quadlet options default to
			// true (or true-when-ReadOnly), so dropping the line re-enables
			// exactly what the user turned off.
			"HttpProxy":     false,
			"ReadOnlyTmpfs": false,
			"StartWithPod":  false,
			"RunInit":       false,
		}},
	}
	vu := eval.UnitRecord{
		Kind: "volume", Stem: "v", Filename: "v.volume",
		Data: map[string]any{"Volume": map[string]any{"UID": int64(0), "GID": int64(0)}},
	}
	bu := eval.UnitRecord{
		Kind: "build", Stem: "b", Filename: "b.build",
		Data: map[string]any{
			"ContainerFile": "FROM scratch\n",
			"Build": map[string]any{
				"ImageTag":  []any{"localhost/b:latest"},
				"ForceRM":   false,
				"TLSVerify": false,
			},
		},
	}
	iu := eval.UnitRecord{
		Kind: "image", Stem: "i", Filename: "i.image",
		Data: map[string]any{"Image": map[string]any{
			"Image": "img", "AllTags": false, "TLSVerify": false,
		}},
	}
	nu := eval.UnitRecord{
		Kind: "network", Stem: "n", Filename: "n.network",
		Data: map[string]any{"Network": map[string]any{"Internal": false, "IPv6": false}},
	}
	files, err := r.BuildFileSet([]eval.Quadlet{{Name: "z", Units: []eval.UnitRecord{cu, vu, bu, iu, nu}}})
	if err != nil {
		t.Fatalf("BuildFileSet: %v", err)
	}
	expect := map[string][]string{
		"z.container": {"StopTimeout=0", "HealthMaxLogCount=0", "Notify=false",
			"HttpProxy=false", "ReadOnlyTmpfs=false", "StartWithPod=false", "RunInit=false"},
		"v.volume":  {"UID=0", "GID=0"},
		"b.build":   {"ForceRM=false", "TLSVerify=false"},
		"i.image":   {"AllTags=false", "TLSVerify=false"},
		"n.network": {"Internal=false", "IPv6=false"},
	}
	for file, wants := range expect {
		for _, want := range wants {
			if !strings.Contains(string(files[file].Content), want) {
				t.Errorf("%s missing %q:\n%s", file, want, files[file].Content)
			}
		}
	}
}

// TestBuildContextModes covers the build-context mode normalization: a plain
// string entry defaults to 0644, a {content, mode} entry keeps its explicit
// mode. This is the only coverage of the mode pipeline (the golden harness
// compares content only).
func TestBuildContextModes(t *testing.T) {
	r := newTestRenderer(t)
	bu := eval.UnitRecord{
		Kind: "build", Stem: "x", Filename: "x.build",
		Data: map[string]any{
			"ContainerFile": "FROM scratch\n",
			"Context": map[string]any{
				"plain.txt": "hello",
				"run.sh":    map[string]any{"content": "#!/bin/sh\n", "mode": "0755"},
			},
			"Build": map[string]any{"ImageTag": []any{"localhost/x:latest"}},
		},
	}
	files, err := r.BuildFileSet([]eval.Quadlet{{Name: "x", Units: []eval.UnitRecord{bu}}})
	if err != nil {
		t.Fatalf("BuildFileSet: %v", err)
	}
	if got := files["images/x.context/plain.txt"].Mode; got != "0644" {
		t.Errorf("plain string entry mode = %q, want 0644", got)
	}
	if got := files["images/x.context/run.sh"].Mode; got != "0755" {
		t.Errorf("explicit-mode entry mode = %q, want 0755", got)
	}
}

// TestEmptyContextTreatedAsAbsent: a present-but-empty Context map must render
// exactly like no Context at all. It writes no context files, so emitting
// SetWorkingDirectory=images/<stem>.context would point the build at a
// directory that never gets created.
func TestEmptyContextTreatedAsAbsent(t *testing.T) {
	r := newTestRenderer(t)
	bu := eval.UnitRecord{
		Kind: "build", Stem: "x", Filename: "x.build",
		Data: map[string]any{
			"ContainerFile": "FROM scratch\n",
			"Context":       map[string]any{},
			"Build":         map[string]any{"ImageTag": []any{"localhost/x:latest"}},
		},
	}
	files, err := r.BuildFileSet([]eval.Quadlet{{Name: "x", Units: []eval.UnitRecord{bu}}})
	if err != nil {
		t.Fatalf("BuildFileSet: %v", err)
	}
	unit := string(files["x.build"].Content)
	if strings.Contains(unit, "SetWorkingDirectory=images/x.context") {
		t.Errorf("empty Context must not point SetWorkingDirectory at a context dir that is never written, got:\n%s", unit)
	}
	if !strings.Contains(unit, "SetWorkingDirectory=unit") {
		t.Errorf("empty Context should fall back to the no-context form (SetWorkingDirectory=unit), got:\n%s", unit)
	}
	for name := range files {
		if strings.HasPrefix(name, "images/x.context/") {
			t.Errorf("empty Context must emit no context files, got %s", name)
		}
	}
}

// TestBuildContextRejectsBadTypes ensures render fails loud on malformed build
// data instead of silently producing an empty file or a default (wrong) mode.
// render validates its inputs rather than trusting the schema to have done so.
func TestBuildContextRejectsBadTypes(t *testing.T) {
	r := newTestRenderer(t)
	withContext := func(ctx map[string]any) []eval.Quadlet {
		return []eval.Quadlet{{Name: "x", Units: []eval.UnitRecord{{
			Kind: "build", Stem: "x", Filename: "x.build",
			Data: map[string]any{
				"Build":   map[string]any{"ImageTag": []any{"localhost/x"}},
				"Context": ctx,
			},
		}}}}
	}
	cases := map[string]struct {
		ctx  map[string]any
		want string
	}{
		"non-string mode":    {map[string]any{"run.sh": map[string]any{"content": "x", "mode": int64(493)}}, "mode"},
		"non-string content": {map[string]any{"f": map[string]any{"content": int64(1)}}, "content"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := r.BuildFileSet(withContext(c.ctx)); err == nil {
				t.Fatal("expected error, got nil")
			} else if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q should mention %q", err, c.want)
			}
		})
	}
}

// TestBuildArtifactPathCollision: two builds with the same stem but different
// filenames don't collide on their unit file, but DO on images/<stem>.*. render
// must catch that itself rather than relying on the CUE-side filename=stem rule.
func TestBuildArtifactPathCollision(t *testing.T) {
	r := newTestRenderer(t)
	mk := func(fn string) eval.UnitRecord {
		return eval.UnitRecord{Kind: "build", Stem: "shared", Filename: fn,
			Data: map[string]any{"ContainerFile": "FROM scratch\n"}}
	}
	quads := []eval.Quadlet{{Name: "q", Units: []eval.UnitRecord{mk("a.build"), mk("b.build")}}}
	if _, err := r.BuildFileSet(quads); err == nil {
		t.Fatal("expected build-artifact collision error, got nil")
	} else if !strings.Contains(err.Error(), "shared.Containerfile") {
		t.Errorf("error should name the colliding path, got: %v", err)
	}
}

// TestBuildFileSetDistinctFilenames is the happy path: no collision.
func TestBuildFileSetDistinctFilenames(t *testing.T) {
	r := newTestRenderer(t)
	quads := []eval.Quadlet{
		{Name: "a", Units: []eval.UnitRecord{containerUnit("img-A", "a")}},
		{Name: "b", Units: []eval.UnitRecord{containerUnit("img-B", "b")}},
	}
	files, err := r.BuildFileSet(quads)
	if err != nil {
		t.Fatalf("BuildFileSet: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d: %v", len(files), files)
	}
}

// TestBuildFileSetRejectsTraversal is a security regression guard: a unit
// filename or a build-context key that escapes the output dir must be refused at
// render time, before it can reach reconcile/apply.
func TestBuildFileSetRejectsTraversal(t *testing.T) {
	r := newTestRenderer(t)

	esc := eval.UnitRecord{
		Kind: "container", Stem: "../escape", Filename: "../escape.container",
		Data: map[string]any{"Container": map[string]any{"Image": "img", "ContainerName": "x"}},
	}
	if _, err := r.BuildFileSet([]eval.Quadlet{{Name: "x", Units: []eval.UnitRecord{esc}}}); err == nil {
		t.Error("BuildFileSet accepted a traversal unit filename, want error")
	}

	bu := eval.UnitRecord{
		Kind: "build", Stem: "b", Filename: "b.build",
		Data: map[string]any{
			"ContainerFile": "FROM scratch\n",
			"Context":       map[string]any{"../../../etc/cron.d/x": "* * * * * root id\n"},
		},
	}
	if _, err := r.BuildFileSet([]eval.Quadlet{{Name: "b", Units: []eval.UnitRecord{bu}}}); err == nil {
		t.Error("BuildFileSet accepted a traversal build-context key, want error")
	}
}

// TestBuildContextKeyMustStayInContextDir: a context key that escapes
// images/<stem>.context/ but lands inside the quadlet root (e.g.
// ../../rogue.container) must be rejected — it would inject a raw managed
// unit file, bypassing the typed schema. (REVIEW-1 finding 4)
func TestBuildContextKeyMustStayInContextDir(t *testing.T) {
	r := newTestRenderer(t)
	bu := eval.UnitRecord{
		Kind: "build", Stem: "b", Filename: "b.build",
		Data: map[string]any{
			"ContainerFile": "FROM scratch\n",
			"Context":       map[string]any{"../../rogue.container": "[Container]\nImage=evil\n"},
		},
	}
	files, err := r.BuildFileSet([]eval.Quadlet{{Name: "b", Units: []eval.UnitRecord{bu}}})
	if err == nil {
		if _, ok := files["rogue.container"]; ok {
			t.Fatal("context key escaped its context dir into a top-level rogue.container")
		}
		t.Fatal("expected an error for a context key escaping images/<stem>.context/")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("error %q should mention the escaped context dir", err)
	}
}
