package primary_name

import "github.com/lugoues/creidhne@v0"

// A primary unit's name is pinned to the quadlet name; setting a different one
// is a unification conflict. You don't name a primary unit, it takes the
// quadlet's name (the meta field IS the unit).
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: {
		name: "other"
		Container: Image: "docker.io/myapp:latest"
	}
}
