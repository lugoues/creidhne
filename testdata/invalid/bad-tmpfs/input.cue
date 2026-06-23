package bad_tmpfs

import "github.com/lugoues/creidhne@v0"

// A non-absolute tmpfs path is rejected by #TmpfsMount.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {Image: "docker.io/app:latest", Tmpfs: ["run:rw"]}
}
