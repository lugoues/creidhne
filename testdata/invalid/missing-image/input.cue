package missing_image

import "github.com/lugoues/creidhne@v0"

// Container with neither Image nor Rootfs -> incomplete.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {ContainerName: "bad"}
}
