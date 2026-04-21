@extern(embed)

package container_service_name

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

_mode:     *"test" | "update" @tag(mode)
_expected: _ @embed(file=expected.quadlets,type=text)

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
	if _mode == "test" {
		expected: _expected
	}
}
