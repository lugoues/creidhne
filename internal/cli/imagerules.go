package cli

import (
	"fmt"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/registry"
)

// imageRuleFindings checks the image registry and its coverage. Static only —
// no registry network access (that is crei image outdated's job):
//
//   - image/unpinned: a registry entry tracking a tag with no digest is not
//     reproducible until the next `crei image pin`.
//   - image/unmanaged: a container whose Image= is a raw registry ref rather
//     than a registry entry's #ref (or a managed .build) receives no update
//     management. Off by default; enable to be nudged to full coverage.
func imageRuleFindings(focus, all []eval.Quadlet, entries []eval.ImageEntry) []ruleFinding {
	var out []ruleFinding

	// refs a container renders when it consumes a registry entry.
	managed := map[string]bool{}
	for _, e := range entries {
		if e.Digest != "" {
			managed[e.Image+"@"+e.Digest] = true
		}
		managed[e.Image] = true // unpinned entries render the bare image
	}

	// Actual sibling-unit filenames, resolved against the whole project. A
	// suffix test alone would also exempt a genuine OCI repository that merely
	// ends in ".build"/".image" (e.g. registry.example/app.image:v1).
	siblingUnits := map[string]bool{}
	for _, q := range all {
		for _, u := range q.Units {
			if u.Kind == "build" || u.Kind == "image" {
				siblingUnits[u.Filename] = true
			}
		}
	}

	for _, e := range entries {
		r, err := registry.Parse(e.Image)
		if err != nil {
			out = append(out, ruleFinding{Rule: "image/unpinned", Unit: "registries/images.cue",
				Message: fmt.Sprintf("%s: invalid image %q: %v", e.Key, e.Image, err)})
			continue
		}
		if r.Tag != "" && e.Digest == "" {
			out = append(out, ruleFinding{Rule: "image/unpinned", Unit: "registries/images.cue",
				Message: fmt.Sprintf("%s (%s) has no digest — not reproducible until 'crei image pin'", e.Key, e.Image)})
		}
	}

	for _, q := range focus {
		for _, u := range q.Units {
			if u.Kind != "container" {
				continue
			}
			img := topStr(u.Data, "imageString")
			switch {
			case img == "":
				continue
			case siblingUnits[img]:
				continue // built/managed by a sibling unit
			case managed[img]:
				continue
			default:
				out = append(out, ruleFinding{Rule: "image/unmanaged", Unit: u.Filename,
					Message: fmt.Sprintf("image %q is not from the registry; it receives no update management (crei image add)", img)})
			}
		}
	}
	return out
}
