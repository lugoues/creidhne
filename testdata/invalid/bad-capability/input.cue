package bad_capability

import "github.com/lugoues/creidhne@v0"

// A typo'd capability is rejected by #Capability.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {Image: "docker.io/app:latest", AddCapability: ["NET_BIND_SERVCE"]}
}
