@extern(embed)

package basic_container

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

_mode: *"test" | "update" @tag(mode)
_expected: _ @embed(file=expected.quadlets,type=text)

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
	if _mode == "test" {
		expected: _expected
	}
}
