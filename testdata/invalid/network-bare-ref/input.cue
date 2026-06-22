package network_bare_ref

import "github.com/lugoues/creidhne@v0"

// A bare .network string is rejected; use #self or externals.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {Image: "docker.io/app:latest", Network: ["some.network"]}
}
