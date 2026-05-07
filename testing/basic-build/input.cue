@extern(embed)

package build_basic

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
			#build: Build: {
				ImageTag: ["localhost/myapp:latest"]
				SetWorkingDirectory: "/src"
				File:                "Containerfile"
			}
		}
	}
	if _mode == "test" {
		expected: _expected
	}
}
