@extern(embed)

package cross_quadlet_ref

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

_mode:     *"test" | "update" @tag(mode)
_expected: _ @embed(file=expected.quadlets,type=text)

// Supporting quadlet: app with a volume and container.
_app: quadlets.#Quadlet & {
	name: "app"
	units: {
		#container: {
			Container: {
				Image:         "docker.io/myapp:latest"
				ContainerName: "app"
				Volume: ["\(_app.units.#volume.#ref):/data"]
				Network: ["proxy-backend.network"]
			}
			Install: WantedBy: ["multi-user.target"]
		}

		#volume: {
			Volume: {}
		}
	}
}

// Subject under test: proxy that references app's #service and network.
test: testing.#Test & {
	subject: quadlets.#Quadlet & {
		name: "proxy"
		units: {
			#container: {
				Container: {
					Image:         "docker.io/haproxy:latest"
					ContainerName: "proxy"
					Network: ["backend.network"]
				}
				Unit: {
					After:    [_app.units.#container.#service]
					Requires: [_app.units.#container.#service]
				}
				Install: WantedBy: ["multi-user.target"]
			}

			networks: backend: {
				Network: Internal: true
			}
		}
	}
	if _mode == "test" {
		expected: _expected
	}
}
