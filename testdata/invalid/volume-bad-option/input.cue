package volume_bad_option

import "github.com/lugoues/creidhne@v0"

// An unknown volume mount option is rejected by #VolumeMountOption.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: {
		volumes: data: {Volume: {}}
		#container: Container: {Image: "docker.io/app:latest", Volume: [units.volumes.data.#self & {target: "/x", options: ["readonly"]}]}
	}
}
