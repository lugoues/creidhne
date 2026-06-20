@extern(embed)

package external_name_override

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

// External units (managed elsewhere) carry name overrides: the map key is the
// CUE-side handle, name is the real unit name. Quadlet-type external "egress"
// -> bare "internet-egress.network" (#ref) and "internet-egress-network.service"
// (#service); native #ExtUnit "db" -> "database.service". The container
// references all three, proving overridden externals render as bare names.
_ext: creidhne.#ExternalUnits & {
	networks: egress: name: "internet-egress"
	services: db: name:     "database"
}

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "app"
		units: #container: {
			Container: {
				Image:         "docker.io/myapp:latest"
				ContainerName: "app"
				Network: ["\(_ext.networks.egress.#ref)"]
			}
			Unit: {
				After: [
					_ext.services.db.#ref,
					_ext.networks.egress.#service,
				]
				Requires: [_ext.services.db.#ref]
			}
			Install: WantedBy: ["multi-user.target"]
		}
	}
	expected: {
		"app.container": _ @embed(file=expected/app.container,type=text)
	}
}
