package artifact_full

import "github.com/lugoues/creidhne@v0"

// A standalone .artifact unit (zero golden coverage before this fixture).
data: creidhne.#Quadlet & {
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
