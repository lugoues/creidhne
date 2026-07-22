// Package registry parses OCI image references and queries registries for the
// current digest of a tracked tag and an image's creation time. It backs the
// crei image commands (pin/outdated); all network access lives here.
package registry

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
)

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

// Status classifies the ref.
func (r Ref) Status() Status {
	switch {
	case r.Tag != "" && r.Digest != "":
		return Managed
	case r.Tag != "":
		return Unpinned
	default:
		return Unmanaged
	}
}

// TaggedRef is "repo:tag" — the channel to resolve.
func (r Ref) TaggedRef() string { return r.Repo + ":" + r.Tag }

// Pinned returns "repo:tag@digest" for a resolved digest, preserving the tag.
func (r Ref) Pinned(digest string) string {
	if r.Tag != "" {
		return r.Repo + ":" + r.Tag + "@" + digest
	}
	return r.Repo + "@" + digest
}

// Digest resolves the current manifest digest of "repo:tag" (a HEAD; no layer
// pull). Auth comes from the ambient docker keychain.
func Digest(repoTag string) (string, error) {
	d, err := crane.Digest(repoTag)
	if err != nil {
		return "", fmt.Errorf("resolve digest for %q: %w", repoTag, err)
	}
	return d, nil
}

// Created returns an image's build time (its config's Created), used for the
// min-age policy. Fetches only the config blob, not layers.
func Created(ref string) (time.Time, error) {
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
