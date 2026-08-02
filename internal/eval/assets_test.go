package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assetProject writes files under a temp dir and returns it.
func assetProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// assetBuild constructs a build UnitRecord whose Context key carries an asset ref.
func assetBuild(key, glob string) UnitRecord {
	return UnitRecord{
		Kind:     "build",
		Stem:     "app",
		Filename: "app.build",
		Data: map[string]any{
			"Context": map[string]any{
				key: map[string]any{"asset": glob},
			},
		},
	}
}

// TestExpandAssetContexts: a recursive glob expands into inline entries under
// the Context key, preserving structure relative to the glob's static prefix,
// deterministically ordered by the map keys themselves.
func TestExpandAssetContexts(t *testing.T) {
	dir := assetProject(t, map[string]string{
		"assets/dash/top.json":        `{"a":1}`,
		"assets/dash/nested/sub.json": `{"b":2}`,
		"assets/dash/skip.txt":        "not json",
	})
	u := assetBuild("dashboards", "assets/dash/**/*.json")
	quads := []Quadlet{{Name: "app", Units: []UnitRecord{u}}}
	if err := expandAssetContexts(dir, quads); err != nil {
		t.Fatal(err)
	}
	ctx := u.Data["Context"].(map[string]any)
	if len(ctx) != 2 {
		t.Fatalf("want 2 expanded entries, got %v", ctx)
	}
	top, ok := ctx["dashboards/top.json"].(map[string]any)
	if !ok || top["content"] != `{"a":1}` || top["mode"] != "0644" {
		t.Fatalf("top.json wrong: %v", ctx["dashboards/top.json"])
	}
	if _, ok := ctx["dashboards/nested/sub.json"]; !ok {
		t.Fatalf("nested structure not preserved: %v", ctx)
	}
	if _, leaked := ctx["dashboards"]; leaked {
		t.Fatal("the asset ref entry must be replaced, not kept")
	}
}

// TestExpandAssetContextsRootKey: a "." key lands files at the context root.
func TestExpandAssetContextsRootKey(t *testing.T) {
	dir := assetProject(t, map[string]string{"conf/app.ini": "x"})
	u := assetBuild(".", "conf/*.ini")
	if err := expandAssetContexts(dir, []Quadlet{{Name: "app", Units: []UnitRecord{u}}}); err != nil {
		t.Fatal(err)
	}
	ctx := u.Data["Context"].(map[string]any)
	if _, ok := ctx["app.ini"]; !ok {
		t.Fatalf(`"." key should place files at the root: %v`, ctx)
	}
}

// TestExpandAssetContextsExecBit: an executable file keeps its exec bit (0755).
func TestExpandAssetContextsExecBit(t *testing.T) {
	dir := assetProject(t, map[string]string{"bin/run.sh": "#!/bin/sh\n"})
	if err := os.Chmod(filepath.Join(dir, "bin", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := assetBuild("scripts", "bin/*.sh")
	if err := expandAssetContexts(dir, []Quadlet{{Name: "app", Units: []UnitRecord{u}}}); err != nil {
		t.Fatal(err)
	}
	e := u.Data["Context"].(map[string]any)["scripts/run.sh"].(map[string]any)
	if e["mode"] != "0755" {
		t.Fatalf("exec bit not preserved: %v", e)
	}
}

// TestExpandAssetContextsErrors: empty matches, escapes, and collisions fail
// loudly; a typo'd glob must never silently ship an empty context.
func TestExpandAssetContextsErrors(t *testing.T) {
	dir := assetProject(t, map[string]string{"assets/a.json": "{}"})
	cases := []struct {
		name string
		u    UnitRecord
		want string
	}{
		{"no matches", assetBuild("x", "assets/typo/**/*.json"), "matches no files"},
		{"absolute", assetBuild("x", "/etc/passwd"), "project-relative"},
		{"escape", assetBuild("x", "../outside/*.json"), "escape"},
	}
	for _, c := range cases {
		err := expandAssetContexts(dir, []Quadlet{{Name: "app", Units: []UnitRecord{c.u}}})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: want error containing %q, got %v", c.name, c.want, err)
		}
	}

	// A resolved file colliding with an existing inline entry is an error, not
	// a silent overwrite.
	u := assetBuild("x", "assets/*.json")
	ctx := u.Data["Context"].(map[string]any)
	ctx["x/a.json"] = "inline wins?"
	err := expandAssetContexts(dir, []Quadlet{{Name: "app", Units: []UnitRecord{u}}})
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("collision must error, got %v", err)
	}
}

// TestAssetEditMovesBuildHash: the reason expansion runs before hashing — an
// asset content change moves the build hash (and so flags consumers).
func TestAssetEditMovesBuildHash(t *testing.T) {
	hashFor := func(content string) string {
		dir := assetProject(t, map[string]string{"assets/dash.json": content})
		u := assetBuild("d", "assets/*.json")
		u.Data["Build"] = map[string]any{"ImageTag": []any{"localhost/app:latest"}}
		quads := []Quadlet{{Name: "app", Units: []UnitRecord{u}}}
		if err := expandAssetContexts(dir, quads); err != nil {
			t.Fatal(err)
		}
		injectBuildHashes(quads)
		return annotationOf(u, "Build")
	}
	h1, h2 := hashFor(`{"v":1}`), hashFor(`{"v":2}`)
	if h1 == "" || h1 == h2 {
		t.Fatalf("an asset edit must move the build hash: %q vs %q", h1, h2)
	}
}

// TestAssetGlobRejectsSymlinkEscape: doublestar follows symlinks, so a
// directory symlink inside the project must not smuggle outside files past
// the lexical ".." check into the build context. (REVIEW-1 finding 5)
func TestAssetGlobRejectsSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.json"), []byte(`{"leak":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := assetProject(t, map[string]string{"assets/real.json": "{}"})
	if err := os.Symlink(outside, filepath.Join(dir, "assets", "leak")); err != nil {
		t.Fatal(err)
	}

	u := assetBuild("x", "assets/**/*.json")
	err := expandAssetContexts(dir, []Quadlet{{Name: "app", Units: []UnitRecord{u}}})
	if err == nil {
		ctx := u.Data["Context"].(map[string]any)
		if _, leaked := ctx["x/leak/secret.json"]; leaked {
			t.Fatal("symlink escape read a file outside the project into the context")
		}
		t.Fatal("want an error for a glob resolving outside the project")
	}
	if !strings.Contains(err.Error(), "outside the project") {
		t.Fatalf("error %q should say the asset resolves outside the project", err)
	}
}
