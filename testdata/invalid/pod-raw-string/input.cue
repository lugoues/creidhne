package pod_raw_string

import "github.com/lugoues/creidhne@v0"

// Pod= is ref-only; a raw string is rejected. Use units.#pod.#self.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {Image: "docker.io/app:latest", Pod: "some.pod"}
}
