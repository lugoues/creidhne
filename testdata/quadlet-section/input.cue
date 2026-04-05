@extern(embed)

package quadlet_section

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "nodeps"
		units: {
			#container: {
				Container: {
					Image: "docker.io/standalone:latest"
				}
				Quadlet: DefaultDependencies: false
				Install: WantedBy: ["default.target"]
			}
		}
	}
	expected: {
		"nodeps.container": _ @embed(file=expected/nodeps.container,type=text)
	}
}
