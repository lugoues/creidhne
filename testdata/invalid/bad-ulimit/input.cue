package bad_ulimit

import "github.com/lugoues/creidhne@v0"

// An unknown ulimit name is rejected by #Ulimit.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {Image: "docker.io/app:latest", Ulimit: ["nofiles=1024"]}
}
