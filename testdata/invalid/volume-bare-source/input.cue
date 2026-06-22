package volume_bare_source

import "github.com/lugoues/creidhne@v0"

// A bare volume-name source is rejected; reference managed volumes via #self.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {Image: "docker.io/app:latest", Volume: ["data.volume:/x"]}
}
