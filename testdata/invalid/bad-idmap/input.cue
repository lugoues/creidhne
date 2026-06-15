package bad_idmap

import "github.com/lugoues/creidhne@v0"

// UIDMap requires container_id:from_id[:amount]; a bare id has no mapping target
// and is rejected by #IDMap.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {
		Image: "docker.io/app:latest"
		UIDMap: ["0"]
	}
}
