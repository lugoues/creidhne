package bad_cidr

import "github.com/lugoues/creidhne@v0"

// Subnet entries must be CIDR notation.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #network: Network: {Subnet: ["not-a-cidr"]}
}
