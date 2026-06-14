package bad_network_driver

import "github.com/lugoues/creidhne@v0"

// Driver is an enum (bridge|macvlan|ipvlan).
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #network: Network: {Driver: "bogus"}
}
