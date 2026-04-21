@extern(embed)

package quadlet_section

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

_mode:     *"test" | "update" @tag(mode)
_expected: _ @embed(file=expected.quadlets,type=text)

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
	if _mode == "test" {
		expected: _expected
	}
}
