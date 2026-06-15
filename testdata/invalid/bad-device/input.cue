package bad_device

import "github.com/lugoues/creidhne@v0"

// AddDevice permissions are a subset of r/w/m; "x" is not a valid permission,
// so #DeviceMapping rejects this entry.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {
		Image: "docker.io/app:latest"
		AddDevice: ["/dev/sda:rwx"]
	}
}
