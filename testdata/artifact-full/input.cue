@extern(embed)

package artifact_full

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

// A standalone .artifact unit (zero golden coverage before this fixture).
test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "data"
		units: #artifact: {
			Artifact: {
				Artifact:  "ghcr.io/example/dataset:v1"
				TLSVerify: true
				Retry:     2
			}
			Install: WantedBy: ["default.target"]
		}
	}
	expected: {
		"data.artifact": _ @embed(file=expected/data.artifact,type=text)
	}
}
