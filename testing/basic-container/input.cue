@extern(embed)

package basic_container

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: quadlets.#Quadlet & {
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
