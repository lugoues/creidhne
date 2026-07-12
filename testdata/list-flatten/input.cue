package list_flatten

import "github.com/lugoues/creidhne"

// Golden coverage for one-level list flattening: helper-composed nested lists
// splice flat on every list property. Label/Tmpfs exercise the CUE
// comprehension path (xStrings run before Go decodes); Environment and
// Unit.After exercise the Go decode path (templates range them directly;
// After also crosses the generated-section + unit_deps overlay types).
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: {
		Container: {
			Image: "docker.io/app:latest"
			Label: ["a=b", ["c=d", "e=f"]]
			Environment: ["X=1", ["Y=2", "Z=3"]]
			Tmpfs: ["/run:size=1m", [{path: "/cache"}]]
		}
		Unit: After: ["a.service", ["b.service", "c.target"]]
	}
}
