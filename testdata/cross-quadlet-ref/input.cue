package cross_quadlet_ref

import "github.com/lugoues/creidhne@v0"

// Two quadlets where one references the other's volume and service.
// Tests that #ref and #service resolve correctly across quadlet boundaries.
// Output is asserted byte-for-byte by the Go golden harness against expected/.

proxy: creidhne.#Quadlet & {
	name: "proxy"
	units: {
		#container: {
			Container: {
				Image:         "docker.io/haproxy:latest"
				ContainerName: "proxy"
				Network: ["backend.network"]
			}
			Unit: {
				After:    [app.units.#container.#service]
				Requires: [app.units.#container.#service]
			}
			Install: WantedBy: ["multi-user.target"]
		}

		networks: backend: {
			Network: Internal: true
		}
	}
}

app: creidhne.#Quadlet & {
	name: "app"
	units: {
		#container: {
			Container: {
				Image:         "docker.io/myapp:latest"
				ContainerName: "app"
				Volume: ["\(app.units.#volume.#ref):/data"]
				Network: ["proxy-backend.network"]
			}
			Install: WantedBy: ["multi-user.target"]
		}

		#volume: {
			Volume: {}
		}
	}
}
