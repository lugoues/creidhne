package example

// import "github.com/lugoues/creidhne"
import "github.com/lugoues/creidhne"

// External systemd units not managed by this config.
externals: creidhne.#ExternalUnits & {
	targets: "network-online": _
	sockets: podman: _
	services: tailscaled: _

	// internet-egress network is managed elsewhere
	networks: "internet-egress": _
}
