package network_full

import "github.com/lugoues/creidhne@v0"

// A full-field .network unit (only a minimal secondary network existed before).
backend: creidhne.#Quadlet & {
	name: "backend"
	units: #network: {
		Network: {
			Driver:   "bridge"
			Subnet:   ["10.89.0.0/24"]
			Gateway:  ["10.89.0.1"]
			IPRange:  ["10.89.0.128-10.89.0.254"]
			IPv6:     true
			Internal: true
			Label:    ["env=prod"]
		}
		Install: WantedBy: ["default.target"]
	}
}
