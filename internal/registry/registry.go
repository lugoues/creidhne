// Package registry parses OCI image references and queries registries for the
// current digest of a tracked tag and an image's creation time. It backs the
// crei image commands (pin/outdated); all network access lives here.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
)

// ParseAge parses a min-age string ("7d", "2w", "12h") into a duration. Empty
// is a zero duration (no minimum).
func ParseAge(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid age %q: want <int>[dwh]", s)
	}
	switch s[len(s)-1] {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid age unit in %q: want d, w, or h", s)
	}
}

// Status classifies a pin by what it carries.
type Status string

const (
	// Managed: a tag (the channel crei tracks) and a digest (what runs).
	Managed Status = "managed"
	// Unpinned: a tag but no digest — not reproducible.
	Unpinned Status = "unpinned"
	// Unmanaged: a digest but no tag — pinned, but crei can't offer updates.
	Unmanaged Status = "unmanaged"
)

// Ref is a parsed image reference split into its channel (repo + tag) and its
// pin (digest). Any of tag/digest may be empty.
type Ref struct {
	Repo   string // registry/repository, no tag or digest
	Tag    string // "" when absent
	Digest string // "sha256:…", "" when absent
}

// Parse splits an OCI ref into repo, tag, and digest. Unlike a plain
// name.ParseReference (which collapses "repo:tag@digest" to a digest ref and
// drops the tag), this keeps both, since the tag is the update channel and the
// digest is what runs.
func Parse(ref string) (Ref, error) {
	var r Ref
	s := ref
	if i := strings.LastIndex(s, "@"); i >= 0 {
		r.Digest = s[i+1:]
		s = s[:i]
	}
	// A tag is the ':' after the last '/', so a "registry:port/repo" port
	// colon (before the '/') is not mistaken for a tag.
	repo := s
	slash := strings.LastIndex(s, "/")
	if c := strings.LastIndex(s, ":"); c > slash {
		r.Tag = s[c+1:]
		repo = s[:c]
	}
	if _, err := name.NewRepository(repo, name.WeakValidation); err != nil {
		return Ref{}, fmt.Errorf("invalid image repository %q: %w", repo, err)
	}
	r.Repo = repo
	return r, nil
}

// Classify labels an entry from whether its image carries a trackable tag and
// whether a digest is pinned. (Digest lives in a separate registry field, so
// it is passed in rather than read from the parsed image ref.)
func Classify(hasTag, hasDigest bool) Status {
	switch {
	case hasTag && hasDigest:
		return Managed
	case hasTag:
		return Unpinned
	default:
		return Unmanaged
	}
}

// TaggedRef is "repo:tag" — the channel to resolve.
func (r Ref) TaggedRef() string { return r.Repo + ":" + r.Tag }

// configureAuth points crane's default keychain at podman's auth.json when
// nothing the keychain checks on its own would be found. The keychain looks at
// ~/.docker/config.json, $DOCKER_CONFIG, $REGISTRY_AUTH_FILE, and
// $XDG_RUNTIME_DIR/containers/auth.json — but under sudo XDG_RUNTIME_DIR is
// typically stripped, and podman's default locations (/run/user/<uid>/ and
// root's /run/containers/0/) are never consulted, so `podman login` creds went
// unused. Setting REGISTRY_AUTH_FILE reuses the keychain's own parsing.
var configureAuth = sync.OnceFunc(func() {
	if p := podmanAuthFallback(os.Getenv, os.Getuid(), fileExists); p != "" {
		if err := os.Setenv("REGISTRY_AUTH_FILE", p); err != nil {
			return // keychain stays anonymous; lookups fail loudly with 401s
		}
	}
})

// podmanAuthFallback returns the podman auth file to use when the default
// keychain would otherwise find nothing ("" to leave things alone).
func podmanAuthFallback(getenv func(string) string, uid int, exists func(string) bool) string {
	if getenv("REGISTRY_AUTH_FILE") != "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && exists(filepath.Join(home, ".docker", "config.json")) {
		return ""
	}
	if dc := getenv("DOCKER_CONFIG"); dc != "" && exists(filepath.Join(dc, "config.json")) {
		return ""
	}
	if x := getenv("XDG_RUNTIME_DIR"); x != "" && exists(filepath.Join(x, "containers", "auth.json")) {
		return ""
	}
	// Podman's defaults the keychain can't see: the per-user runtime dir
	// (when XDG_RUNTIME_DIR is stripped, e.g. under sudo) and root's.
	candidates := []string{fmt.Sprintf("/run/user/%d/containers/auth.json", uid)}
	if uid == 0 {
		candidates = append(candidates, "/run/containers/0/auth.json")
	}
	for _, p := range candidates {
		if exists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Digest resolves the current manifest digest of "repo:tag" (a HEAD; no layer
// pull). Auth comes from the docker/podman keychain (see configureAuth).
func Digest(repoTag string) (string, error) {
	configureAuth()
	d, err := crane.Digest(repoTag)
	if err != nil {
		return "", fmt.Errorf("resolve digest for %q: %w", repoTag, err)
	}
	return d, nil
}

// Created returns an image's build time (its config's Created), used for the
// min-age policy. Fetches only the config blob, not layers.
func Created(ref string) (time.Time, error) {
	configureAuth()
	img, err := crane.Pull(ref)
	if err != nil {
		return time.Time{}, fmt.Errorf("pull config for %q: %w", ref, err)
	}
	cf, err := img.ConfigFile()
	if err != nil {
		return time.Time{}, fmt.Errorf("read config for %q: %w", ref, err)
	}
	return cf.Created.Time, nil
}
