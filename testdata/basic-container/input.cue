@extern(embed)

package basic_container

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "nginx"
		units: {
			#container: {
				Container: {
					Image:         "docker.io/nginx:latest"
					ContainerName: "nginx"
				}
				Install: WantedBy: ["default.target"]
			}
		}
	}
	expected: {
		"nginx.container": _ @embed(file=expected/nginx.container,type=text)
	}
}
