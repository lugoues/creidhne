@extern(embed)

package build_basic

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
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
