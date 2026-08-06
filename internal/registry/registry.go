// Package registry parses OCI image references and queries registries for the
// current digest of a tracked tag and an image's creation time. It backs the
// crei image commands (pin/outdated); all network access lives here.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
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
	// Validate the tag and digest portions too: an invalid one written back to
	// registries/images.cue would fail schema validation on every later load,
	// wedging the registry until hand-edited.
	if r.Tag != "" {
		if _, err := name.NewTag(repo+":"+r.Tag, name.WeakValidation); err != nil {
			return Ref{}, fmt.Errorf("invalid image tag %q: %w", r.Tag, err)
		}
	}
	if r.Digest != "" {
		if _, err := name.NewDigest(repo+"@"+r.Digest, name.WeakValidation); err != nil {
			return Ref{}, fmt.Errorf("invalid image digest %q: %w", r.Digest, err)
		}
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

// Tags lists a repository's tags (for semver-range selection).
func Tags(repo string) ([]string, error) {
	configureAuth()
	tags, err := crane.ListTags(repo)
	if err != nil {
		return nil, fmt.Errorf("list tags for %q: %w", repo, err)
	}
	return tags, nil
}

// Version is a parsed version-shaped tag: an optional "v", dotted numerics
// of any component count (semver and CalVer alike: "8.25.3", "2026.7.7.2"),
// and an optional "-suffix". Per the container convention (and Renovate's
// rule) a suffix means compatibility ("1.2.0-alpine"), not pre-release:
// candidates must carry the identical suffix and the suffix never changes.
type Version struct {
	Raw    string // literal registry form
	Parts  []int
	Suffix string // "" or e.g. "alpine"
}

var versionTag = regexp.MustCompile(`^v?([0-9]+(?:\.[0-9]+)*)(?:-([0-9A-Za-z.]+))?$`)

// ParseVersion reports whether a tag is version-shaped, and parses it.
func ParseVersion(tag string) (Version, bool) {
	m := versionTag.FindStringSubmatch(tag)
	if m == nil {
		return Version{}, false
	}
	v := Version{Raw: tag, Suffix: m[2]}
	for _, p := range strings.Split(m[1], ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, false // component overflow etc.
		}
		v.Parts = append(v.Parts, n)
	}
	// A suffix that itself parses as a bare number is more likely a version
	// component oddity than a compatibility name; still treated as suffix —
	// same-suffix comparison keeps it safe either way.
	return v, true
}

// Compare orders by numeric tuple, shorter tuples padded with zeros
// ("2026.7.7.2" > "2026.7.7"; "8.25" == "8.25.0"). Suffixes do not order
// (callers compare only same-suffix versions).
func (v Version) Compare(o Version) int {
	n := max(len(v.Parts), len(o.Parts))
	for i := 0; i < n; i++ {
		a, b := 0, 0
		if i < len(v.Parts) {
			a = v.Parts[i]
		}
		if i < len(o.Parts) {
			b = o.Parts[i]
		}
		if a != b {
			if a < b {
				return -1
			}
			return 1
		}
	}
	return 0
}

// truncated returns the version cut to at most n components (for comparing a
// deeper candidate at the current tag's precision).
func (v Version) truncated(n int) Version {
	if len(v.Parts) <= n {
		return v
	}
	return Version{Raw: v.Raw, Parts: v.Parts[:n], Suffix: v.Suffix}
}

// inRange checks a Masterminds semver constraint against the first three
// tuple components (missing ones zero). Components past the third rank in
// Compare but are invisible to constraints — full constraint algebra over
// arbitrary tuples is not worth reimplementing.
func (v Version) inRange(c *semver.Constraints) bool {
	p := func(i int) uint64 {
		if i < len(v.Parts) {
			return uint64(v.Parts[i])
		}
		return 0
	}
	return c.Check(semver.New(p(0), p(1), p(2), "", ""))
}

// PickVersion selects the best upgrade candidate from a tag list: the
// highest version-shaped tag with the same suffix as current, strictly
// newer than current, and (when constraint is non-empty) inside the range.
// "" when there is no such candidate. The implicit rangeless behavior is
// therefore ">= current": the tag itself states the floor, the range (if
// any) states the ceiling, and "=x.y.z" freezes.
func PickVersion(tags []string, current Version, constraint string) (string, error) {
	var c *semver.Constraints
	if constraint != "" {
		var err error
		if c, err = semver.NewConstraint(constraint); err != nil {
			return "", fmt.Errorf("invalid semver range %q: %w", constraint, err)
		}
	}
	best := Version{}
	found := false
	for _, t := range tags {
		v, ok := ParseVersion(t)
		if !ok || v.Suffix != current.Suffix {
			continue
		}
		// Precision guard (Renovate-informed): a candidate less precise than
		// current is a different scheme, not an upgrade ("3.20" must never
		// jump to alpine's date-stamp "20260127"); more precise is fine (a
		// CalVer hotfix "2026.7.7.2" over "2026.7.7").
		if len(v.Parts) < len(current.Parts) {
			continue
		}
		// Date-tag guard: a first component leaping into 5+ digits when the
		// current one is small is a date stamp (20260127), not a major.
		if v.Parts[0] >= 10000 && current.Parts[0] < 1000 {
			continue
		}
		// The candidate truncated to the current tag's precision must be
		// strictly newer. A deeper tag refining the same version is an alias
		// narrowing, not an upgrade (debian:13-slim -> 13.6-slim is the same
		// image; taking it would silently stop floating on the major), while
		// a genuinely newer deep tag still passes (2026.7.7.2 over
		// 2026.6.19: 2026.7.7 > 2026.6.19).
		if v.truncated(len(current.Parts)).Compare(current) <= 0 {
			continue
		}
		if c != nil && !v.inRange(c) {
			continue
		}
		if !found || v.Compare(best) > 0 {
			best, found = v, true
		}
	}
	if !found {
		return "", nil
	}
	return best.Raw, nil
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
