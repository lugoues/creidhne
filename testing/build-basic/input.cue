@extern(embed)

package build_basic

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

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
	expected: {
		"myapp.build": _ @embed(file=expected/myapp.build,type=text)
	}
}
