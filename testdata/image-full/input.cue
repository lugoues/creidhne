@extern(embed)

package image_full

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

// A standalone .image unit (zero golden coverage before this fixture).
test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "cache"
		units: #image: {
			Image: {
				Image:     "docker.io/library/redis:7"
				Policy:    "newer"
				Arch:      "arm64"
				TLSVerify: true
				Retry:     3
			}
			Install: WantedBy: ["default.target"]
		}
	}
	expected: {
		"cache.image": _ @embed(file=expected/cache.image,type=text)
	}
}
