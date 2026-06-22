package volume_foreign_self

import "github.com/lugoues/creidhne@v0"

// A network's #self cannot occupy a Volume= slot (wrong _kind).
bad: creidhne.#Quadlet & {
	name: "bad"
	units: {
		networks: net: {Network: {}}
		#container: Container: {Image: "docker.io/app:latest", Volume: [units.networks.net.#self]}
	}
}
