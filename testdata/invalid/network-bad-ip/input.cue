package network_bad_ip

import "github.com/lugoues/creidhne@v0"

// An invalid IP in network connection options is rejected by #IPv4.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: {
		networks: net: {Network: {}}
		#container: Container: {Image: "docker.io/app:latest", Network: [units.networks.net.#self & {ip: "not-an-ip"}]}
	}
}
