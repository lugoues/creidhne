package creidhne

import "list"

#Network: {
	name: string
	// _stem is injected by #Units; identity is computed inline from it.
	_stem:    string
	#ref:     "\(_stem).network"
	#service: "\(_stem)-network.service"

	// #self: reference handle for a Network= field, optionally decorated with
	// connection options: units.networks.X.#self & {ip: "10.0.0.5", alias: ["web"]}.
	#self: #NetworkSelf & {source: #ref}

	// #networkName resolves to the explicit NetworkName, else systemd-%N.
	#networkName: *Network.NetworkName | "systemd-\(_stem)"

	Unit?:    #UnitSection
	Service?: #ServiceSection
	Install?: #InstallSection
	Quadlet?: #QuadletSection

	Network?: {
		// Override the default network name. Defaults to systemd-%N.
		NetworkName?: string
		// Override the default systemd service unit name.
		ServiceName?: string
		// Driver to manage the network. Currently bridge, macvlan and ipvlan are supported.
		Driver?: #NetworkDriver
		// Set the IPAM driver (IP Address Management Driver) for the network.
		IPAMDriver?: #IPAMDriver
		// Set driver specific options.
		Options?: [...(string | [...string])]
		// For bridge: the bridge name. For macvlan/ipvlan: the parent device.
		InterfaceName?: string
		// The subnet in CIDR notation.
		Subnet?: [...(#CIDR | [...#CIDR])]
		// Define a gateway for the subnet. Requires a Subnet option.
		Gateway?: [...(#IPAddress | [...#IPAddress])]
		// Allocate container IP from a range. Accepts CIDR or startIP-endIP syntax.
		IPRange?: [...(#IPRange | [...#IPRange])]
		// Enable IPv6 (Dual Stack) networking.
		IPv6?: bool
		// Restrict external access of this network.
		Internal?: bool
		// If enabled, disables the DNS plugin for this network.
		DisableDNS?: bool
		// When true, the network is deleted when the service is stopped.
		NetworkDeleteOnStop?: bool
		// Set network-scoped DNS resolver/nameserver for containers in this network.
		DNS?: [...(#IPAddress | [...#IPAddress])]
		// Set one or more OCI labels on the network. A raw "key=value" string, or
		// a #Rendered helper (e.g. #JSONLabel) that computes one.
		Label?: [...(#LabelValue | [...#LabelValue])]
		// Arguments passed directly between "podman" and "network" for unsupported features.
		GlobalArgs?: [...(string | [...string])]
		// Arguments passed directly to the end of the podman network create command.
		PodmanArgs?: [...(string | [...string])]
		// Load the specified containers.conf(5) module.
		ContainersConfModule?: [...(string | [...string])]
	}

	// Resolved labels: raw strings pass through; #Rendered helpers contribute
	// their #rendered, one label or a spliced list (_#renderLabel).
	labelStrings: list.Concat([
		if Network.Label != _|_ for e in Network.Label {
			list.Concat([
				if (e & [...]) == _|_ {(_#renderLabel & {#e: e}).out},
				if (e & [...]) != _|_ for l in (e & [...]) {(_#renderLabel & {#e: l}).out},
			])
		},
	])
}
