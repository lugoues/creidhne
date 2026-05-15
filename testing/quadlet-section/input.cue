@extern(embed)

package quadlet_section

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: quadlets.#Quadlet & {
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
