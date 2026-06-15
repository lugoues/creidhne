// Package kinds is the single source of truth for the set of Quadlet unit kinds
// and their file extensions. Both the renderer (which templates to run, how to
// name files) and the reconciler (which on-disk files it manages and prunes)
// derive from it, so they cannot silently disagree. A kind known to one but not
// the other would otherwise be written but never pruned, or vice versa.
package kinds

import "sort"

// ext maps each unit kind to its Quadlet file extension. It is unexported and
// reached only through Kinds and Extensions, which return fresh copies, so a
// caller cannot mutate the shared registry and make the renderer and reconciler
// disagree.
var ext = map[string]string{
	"container": ".container",
	"pod":       ".pod",
	"volume":    ".volume",
	"network":   ".network",
	"kube":      ".kube",
	"build":     ".build",
	"image":     ".image",
	"artifact":  ".artifact",
}

// Kinds returns the managed unit kinds, sorted. The returned slice is a fresh
// copy; mutating it does not affect the registry.
func Kinds() []string {
	out := make([]string, 0, len(ext))
	for k := range ext {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Extensions returns the set of managed file extensions. The returned map is a
// fresh copy; mutating it does not affect the registry.
func Extensions() map[string]bool {
	exts := make(map[string]bool, len(ext))
	for _, e := range ext {
		exts[e] = true
	}
	return exts
}
