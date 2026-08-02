package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/lugoues/creidhne/internal/eval"
)

// updateEmit regenerates the emitter golden files:
//
//	go test ./internal/cli -run TestEmitGolden -update-emit
//
// Review the diff before committing: these files ARE the emitters' byte
// contract. registries/*.cue are git-tracked, crei-rewritten files, so any
// formatting drift here (header, ordering, one-line vs expanded threshold)
// churns every user's git diff on their next pin/update/create.
var updateEmit = flag.Bool("update-emit", false, "rewrite the emitter golden files")

func checkEmitGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateEmit {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (regenerate with -update-emit): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s drifted from its golden; a byte change here churns every user's registries/ git diff on the next write-back.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// TestEmitGoldenImages byte-locks emitImageRegistry over every entry shape:
// unpinned, pinned, minAge, range, locked, and a non-identifier key (quoted).
func TestEmitGoldenImages(t *testing.T) {
	got, err := emitImageRegistry([]eval.ImageEntry{
		{Key: "tracker", Image: "docker.io/library/redis:7.2"},
		{Key: "app", Image: "ghcr.io/acme/app:2.3.0", Digest: "sha256:6339c0ffee"},
		{Key: "cautious", Image: "ghcr.io/acme/slow:1.0.0", Digest: "sha256:aa11", MinAge: "7d"},
		{Key: "ranged", Image: "docker.io/x/z:8.25.0", Digest: "sha256:bb22", Range: "~8.25"},
		{Key: "held", Image: "docker.io/library/traefik:v3.6", Digest: "sha256:cc33",
			Lock: &eval.ImageLock{Reason: "3.6 breaks websocket upgrades", Since: "2026-07-24"}},
		{Key: "weird-key", Image: "docker.io/x/w:1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkEmitGolden(t, "images.cue.golden", got)
}

// TestEmitGoldenSecrets byte-locks emitSecretRegistry over every entry shape:
// bare declaration, generate (length; length+charset), name override, and
// name override plus policy.
func TestEmitGoldenSecrets(t *testing.T) {
	got, err := emitSecretRegistry([]eval.SecretEntry{
		{Key: "plain", Name: "plain"},
		{Key: "db_password", Name: "db_password", Generate: &eval.SecretGenerate{Length: 40}},
		{Key: "api_key", Name: "api_key", Generate: &eval.SecretGenerate{Length: 24, Charset: "hex"}},
		{Key: "tls", Name: "tls-cert"},
		{Key: "session", Name: "session-key", Generate: &eval.SecretGenerate{Length: 64, Charset: "base64"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkEmitGolden(t, "secrets.cue.golden", got)
}
