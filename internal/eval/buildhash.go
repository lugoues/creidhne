package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// buildHashAnnotation is the annotation key carrying a build's content hash. It
// is stamped on the build unit (so a Containerfile/context change alters the
// .build file and flags the build stale through the normal per-file mechanism)
// and on every container that consumes the built image (so the container is
// flagged when the image underneath it is rebuilt — its own config is
// otherwise unchanged, and crei tracks no image identity). The k8s
// pod-template-hash idea: fold the inputs into a version that rides the file.
const buildHashAnnotation = "creidhne.build-hash"

// injectBuildHashes stamps each build's content hash onto the build unit and
// its consuming containers. Runs once over the whole project (all quadlets in
// scope), so cross-quadlet image references resolve and every render subset
// sees identical, already-stamped data.
//
// A tag produced by two different builds is a hard error: consumers reference
// the tag string alone, so the hash (and the After= ordering below) would be
// associated with whichever build the map visited last.
func injectBuildHashes(quads []Quadlet) error {
	// Pass 1: hash each build's pristine inputs (before any stamping). The hash
	// covers the entire build data (Containerfile, context, BuildArg, ImageTag,
	// ...), so any change that would produce a different image moves it. Key it
	// by every string a consumer might use for Image=: the build's own
	// ref/filename (Image=<stem>.build) and each ImageTag (Image=<tag>, the
	// natural form, and the only one that works cross-quadlet where the .build
	// unit is not referenceable).
	hashes := map[string]string{}
	tagService := map[string]string{} // tag -> producing build's service
	tagOwner := map[string]string{}   // tag -> producing build's filename
	for _, q := range quads {
		for _, u := range q.Units {
			if u.Kind != "build" {
				continue
			}
			h := hashData(u.Data)
			hashes[u.Filename] = h
			for _, tag := range buildImageTags(u.Data) {
				if prev, ok := tagOwner[tag]; ok && prev != u.Filename {
					return fmt.Errorf("image tag %q is produced by both %s and %s: consumers reference the tag alone, so staleness and ordering would follow an arbitrary one of them", tag, prev, u.Filename)
				}
				tagOwner[tag] = u.Filename
				hashes[tag] = h
				tagService[tag] = u.Service
			}
		}
	}
	if len(hashes) == 0 {
		return nil
	}

	// Pass 2: stamp. The build carries its own hash; a container carries the
	// hash of the build its Image resolves to. A tag-matched consumer also
	// gains an After= on the producing build: quadlet auto-wires ordering for
	// Image=<stem>.build references but knows nothing about raw tags, and
	// without it a restart --stale can restart the container against the old
	// image while the rebuild is still running.
	for _, q := range quads {
		for _, u := range q.Units {
			switch u.Kind {
			case "build":
				stampAnnotation(u.Data, "Build", hashes[u.Filename])
			case "container":
				img, _ := u.Data["imageString"].(string)
				if img == "" {
					continue
				}
				h, ok := hashes[img]
				if !ok {
					continue
				}
				stampAnnotation(u.Data, "Container", h)
				if svc := tagService[img]; svc != "" && svc != u.Service {
					ensureAfter(u.Data, svc)
				}
			}
		}
	}
	return nil
}

// ensureAfter adds svc to the unit's [Unit] After= list unless some form of the
// list (flat or nested) already names it, creating the Unit section if needed.
func ensureAfter(data map[string]any, svc string) {
	unit, ok := data["Unit"].(map[string]any)
	if !ok {
		unit = map[string]any{}
		data["Unit"] = unit
	}
	existing, _ := unit["After"].([]any)
	for _, e := range existing {
		switch v := e.(type) {
		case string:
			if v == svc {
				return
			}
		case []any:
			for _, n := range v {
				if s, ok := n.(string); ok && s == svc {
					return
				}
			}
		}
	}
	unit["After"] = append(existing, svc)
}

// buildImageTags extracts a build's ImageTag values from its data. The schema
// types ImageTag as [...(string | [...string])], so entries are strings or
// nested string lists; both are flattened. These are the image names a consumer
// references with Image=<tag>.
func buildImageTags(data map[string]any) []string {
	build, ok := data["Build"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := build["ImageTag"].([]any)
	if !ok {
		return nil
	}
	var tags []string
	for _, e := range raw {
		switch v := e.(type) {
		case string:
			tags = append(tags, v)
		case []any:
			for _, s := range v {
				if str, ok := s.(string); ok {
					tags = append(tags, str)
				}
			}
		}
	}
	return tags
}

// hashData is a stable short hash of a unit's data. json.Marshal sorts map
// keys, so the encoding (and thus the hash) is deterministic for equal data.
func hashData(data map[string]any) string {
	b, err := json.Marshal(normalizeForHash(data))
	if err != nil {
		// Unit data is decoded JSON; it always re-marshals. A hash of nothing
		// is still stable, so degrade rather than fail the load.
		b = nil
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// normalizeForHash replaces every invalid-UTF-8 string (asset context entries
// read from disk can carry arbitrary bytes) with a digest marker before JSON
// encoding. encoding/json substitutes U+FFFD for invalid bytes, so two binary
// contents differing only in those bytes would otherwise encode — and hash —
// identically, leaving the build unstale after a real context change. Valid
// strings pass through untouched, so hashes of all-text builds are unchanged.
func normalizeForHash(v any) any {
	switch x := v.(type) {
	case string:
		return hashableString(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[hashableString(k)] = normalizeForHash(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalizeForHash(val)
		}
		return out
	default:
		return v
	}
}

func hashableString(s string) string {
	if utf8.ValidString(s) && !strings.ContainsRune(s, utf8.RuneError) {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	return "!binary:sha256:" + hex.EncodeToString(sum[:])
}

// stampAnnotation appends "creidhne.build-hash=<hash>" to a section's
// Annotation list, creating the list if absent. Appended last so it never
// perturbs the order of user annotations.
func stampAnnotation(data map[string]any, section, hash string) {
	sec, ok := data[section].(map[string]any)
	if !ok {
		return
	}
	existing, _ := sec["Annotation"].([]any)
	sec["Annotation"] = append(existing, buildHashAnnotation+"="+hash)
}
