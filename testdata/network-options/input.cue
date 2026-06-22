package network_options

import "github.com/lugoues/creidhne"

// End-to-end golden coverage for Network= connection options: a .network #self
// decorated with ip/alias/mac, a bridge struct with an interface name, and a
// bare mode.
svc: creidhne.#Quadlet & {
	name: "svc"
	units: {
		networks: net: {Network: {}}
		#container: {
			Container: {
				Image:         "docker.io/app:latest"
				ContainerName: "svc"
				Network: [
					units.networks.net.#self & {ip: "10.89.0.5", alias: ["web", "app"], mac: "02:42:ac:11:00:02"},
					{mode: "bridge", interface_name: "eth1"},
					"host",
				]
			}
			Install: WantedBy: ["default.target"]
		}
	}
}
