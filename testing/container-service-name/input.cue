@extern(embed)

package container_service_name

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: quadlets.#Quadlet & {
		name: "myapp"
		units: {
			#container: {
				Container: {
					Image:       "docker.io/myapp:latest"
					ServiceName: "custom-myapp"
				}
				Install: WantedBy: ["default.target"]
			}
		}
	}
	expected: {
		"myapp.container": _ @embed(file=expected/myapp.container,type=text)
	}
}
