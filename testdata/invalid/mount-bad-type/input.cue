package mount_bad_type

import "github.com/lugoues/creidhne@v0"

// An unknown --mount type is rejected by #MountType.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {Image: "docker.io/app:latest", Mount: [creidhne.#MountSpec & {type: "bnid", destination: "/x"}]}
}
