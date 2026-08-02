package creidhne_test

import (
	"os"
	"regexp"
	"testing"
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
