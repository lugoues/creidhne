package list_flatten_depth2

import "github.com/lugoues/creidhne"

// Nesting is exactly one level; a doubly nested list is a type error.
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: Container: {
		Image: "docker.io/x"
		Label: [[["x=y"]]]
	}
}
