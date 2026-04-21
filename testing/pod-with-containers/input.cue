@extern(embed)

package pod_with_containers

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

_mode:     *"test" | "update" @tag(mode)
_expected: _ @embed(file=expected.quadlets,type=text)

_externals: quadlets.#ExternalUnits & {
	services: tailscaled: _
}

test: testing.#Test & {
	subject: quadlets.#Quadlet & {
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
	if _mode == "test" {
		expected: _expected
	}
}
