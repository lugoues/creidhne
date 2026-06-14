@extern(embed)

package pod_with_containers

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

_externals: creidhne.#ExternalUnits & {
	services: tailscaled: _
}

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "webapp"
		units: {
			#pod: {
				Unit: {
					After:    [_externals.services.tailscaled.#ref]
					Requires: [_externals.services.tailscaled.#ref]
				}
				Pod: {
					PodName:     "webapp"
					PublishPort: ["0.0.0.0:8080:8080"]
				}
			}

			#container: {
				Container: {
					Image:         "docker.io/myapp:latest"
					ContainerName: "webapp-app"
					Pod:           test.subject.units.#pod.#ref
				}
				Install: WantedBy: ["multi-user.target"]
			}

			containers: sidecar: {
				Container: {
					Image:         "docker.io/envoy:latest"
					ContainerName: "webapp-sidecar"
					Pod:           test.subject.units.#pod.#ref
				}
				Install: WantedBy: ["multi-user.target"]
			}
		}
	}
	expected: {
		"webapp-sidecar.container": _ @embed(file=expected/webapp-sidecar.container,type=text)
		"webapp.container": _ @embed(file=expected/webapp.container,type=text)
		"webapp.pod": _ @embed(file=expected/webapp.pod,type=text)
	}
}
