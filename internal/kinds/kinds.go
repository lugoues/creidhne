// Package kinds is the single source of truth for the set of Quadlet unit kinds
// and their file extensions. Both the renderer (which templates to run, how to
// name files) and the reconciler (which on-disk files it manages and prunes)
// derive from it, so they cannot silently disagree. A kind known to one but not
// the other would otherwise be written but never pruned, or vice versa.
package kinds

// Ext maps each unit kind to its Quadlet file extension.
var Ext = map[string]string{
	"container": ".container",
	"pod":       ".pod",
	"volume":    ".volume",
	"network":   ".network",
	"kube":      ".kube",
	"build":     ".build",
	"image":     ".image",
	"artifact":  ".artifact",
}

// Extensions returns the set of managed file extensions derived from Ext.
func Extensions() map[string]bool {
	exts := make(map[string]bool, len(Ext))
	for _, e := range Ext {
		exts[e] = true
	}
	return exts
}
