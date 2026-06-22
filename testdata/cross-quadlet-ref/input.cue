@extern(embed)

package cross_quadlet_ref

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

// Two quadlets where one references the other's volume and service. Tests that
// #ref and #service resolve correctly across quadlet boundaries. Output is
// asserted byte-for-byte by the Go golden harness against expected/.
test: testing.#Test & {
	subjects: {
		proxy: creidhne.#Quadlet & {
			name: "proxy"
			units: {
				#container: {
					Container: {
						Image:         "docker.io/haproxy:latest"
						ContainerName: "proxy"
						Network: [units.networks.backend.#self]
					}
					Unit: {
						After:    [test.subjects.app.units.#container.#service]
						Requires: [test.subjects.app.units.#container.#service]
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
						Volume: [test.subjects.app.units.#volume.#self & {target: "/data"}]
						Network: [test.subjects.proxy.units.networks.backend.#self]
					}
					Install: WantedBy: ["multi-user.target"]
				}

				#volume: {
					Volume: {}
				}
			}
		}
	}
	expected: {
		"proxy.container": _ @embed(file=expected/proxy.container,type=text)
		"proxy-backend.network": _ @embed(file=expected/proxy-backend.network,type=text)
		"app.container": _ @embed(file=expected/app.container,type=text)
		"app.volume": _ @embed(file=expected/app.volume,type=text)
	}
}
