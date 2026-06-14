package image_full

import "github.com/lugoues/creidhne@v0"

// A standalone .image unit (zero golden coverage before this fixture).
cache: creidhne.#Quadlet & {
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
