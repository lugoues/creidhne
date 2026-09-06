package creidhne_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	creidhne "github.com/lugoues/creidhne"
)

// TestReadmeInstallVersionsAgree: the README shows the release version in two
// places (the mise tool entry and the download script). They drifted apart
// once (1.9.0 vs 1.0.1); this pins them to each other so a bump edits both.
// (REVIEW-1 finding 7)
func TestReadmeInstallVersionsAgree(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	miseRe := regexp.MustCompile(`"github:lugoues/creidhne"\s*=\s*"([0-9.]+)"`)
	scriptRe := regexp.MustCompile(`(?m)^ver=([0-9.]+)$`)
	mise := miseRe.FindSubmatch(raw)
	script := scriptRe.FindSubmatch(raw)
	if mise == nil || script == nil {
		t.Fatalf("README install examples not found (mise=%v script=%v); update this test alongside the README", mise != nil, script != nil)
	}
	if got, want := string(script[1]), string(mise[1]); got != want {
		t.Fatalf("README install versions disagree: mise example says %s, download script says %s", want, got)
	}
}

// The example project ships a copy of the config schema (what `crei init`
// writes) so editors validate offline. It is a build artifact checked into the
// tree, so nothing but this test stops it drifting from the embedded original
// when a config key is added.
func TestExampleConfigSchemaMatchesEmbedded(t *testing.T) {
	onDisk, err := os.ReadFile(filepath.Join("example", ".crei", "config.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(onDisk, creidhne.ConfigSchema) {
		t.Error("example/.crei/config.schema.json is stale; copy crei.schema.json over it")
	}
}
