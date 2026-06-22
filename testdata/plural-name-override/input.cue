@extern(embed)

package plural_name_override

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

// Plural units exercise both the name override and the *Name default
// (key-as-name). Volume "data" overrides its name to "cache" (stem app-cache);
// volume "logs" takes its key as the name (stem app-logs); network "mesh"
// overrides to "internal" (stem app-internal). The primary container
// cross-references all three, proving the override and the default both flow
// through #ref/#service into rendered output.
test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "app"
		units: {
			#container: {
				Container: {
					Image:         "docker.io/myapp:latest"
					ContainerName: "app"
					Volume: [
						test.subject.units.volumes.data.#self & {target: "/cache"},
						test.subject.units.volumes.logs.#self & {target: "/logs"},
					]
					Network: ["\(test.subject.units.networks.mesh.#ref)"]
				}
				Unit: After: [test.subject.units.volumes.data.#service]
				Install: WantedBy: ["multi-user.target"]
			}

			// name override: key "data" -> name "cache" -> stem "app-cache"
			volumes: data: {
				name: "cache"
				Volume: Label: ["role=cache"]
			}

			// *Name default: no name -> key "logs" -> stem "app-logs"
			volumes: logs: Volume: {}

			// name override on a network: key "mesh" -> name "internal" -> stem "app-internal"
			networks: mesh: {
				name: "internal"
				Network: Internal: true
			}
		}
	}
	expected: {
		"app.container": _ @embed(file=expected/app.container,type=text)
		"app-cache.volume": _ @embed(file='expected/app-cache.volume',type=text)
		"app-internal.network": _ @embed(file='expected/app-internal.network',type=text)
		"app-logs.volume": _ @embed(file='expected/app-logs.volume',type=text)
	}
}
