package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
func injectBuildHashes(quads []Quadlet) {
	// Pass 1: hash each build's pristine inputs (before any stamping). The hash
	// covers the entire build data (Containerfile, context, BuildArg, ImageTag,
	// ...), so any change that would produce a different image moves it. Key it
	// by every string a consumer might use for Image=: the build's own
	// ref/filename (Image=<stem>.build) and each ImageTag (Image=<tag>, the
	// natural form, and the only one that works cross-quadlet where the .build
	// unit is not referenceable).
	hashes := map[string]string{}
	for _, q := range quads {
		for _, u := range q.Units {
			if u.Kind == "build" {
				h := hashData(u.Data)
				hashes[u.Filename] = h
				for _, tag := range buildImageTags(u.Data) {
					hashes[tag] = h
				}
			}
		}
	}
	if len(hashes) == 0 {
		return
	}

	// Pass 2: stamp. The build carries its own hash; a container carries the
	// hash of the build its Image resolves to.
	for _, q := range quads {
		for _, u := range q.Units {
			switch u.Kind {
			case "build":
				stampAnnotation(u.Data, "Build", hashes[u.Filename])
			case "container":
				if img, _ := u.Data["imageString"].(string); img != "" {
					if h, ok := hashes[img]; ok {
						stampAnnotation(u.Data, "Container", h)
					}
				}
			}
		}
	}
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
	b, err := json.Marshal(data)
	if err != nil {
		// Unit data is decoded JSON; it always re-marshals. A hash of nothing
		// is still stable, so degrade rather than fail the load.
		b = nil
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
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
