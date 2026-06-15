package bad_volume

import "github.com/lugoues/creidhne@v0"

// A Volume container directory must be absolute, so "myvol:data" (non-absolute
// destination) is rejected by #VolumeMount.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {
		Image: "docker.io/app:latest"
		Volume: ["myvol:data"]
	}
}
