package example

// import "github.com/lugoues/quadlets@v0"
import "github.com/lugoues/quadlets"

// External systemd units not managed by this config.
externals: quadlets.#ExternalUnits & {
	targets: "network-online": _
	sockets: podman: _
	services: tailscaled: _

	// internet-egress network is managed elsewhere
	networks: "internet-egress": _
}
