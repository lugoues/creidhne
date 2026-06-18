package creidhne

#Network: {
	// #stem is injected by #Units; identity is computed inline from it.
	#stem:    string
	#ref:     "\(#stem).network"
	#service: "\(#stem)-network.service"

	// #networkName resolves to the explicit NetworkName, else systemd-%N.
	#networkName: *Network.NetworkName | "systemd-\(#stem)"

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
		Options?: [...string]
		// For bridge: the bridge name. For macvlan/ipvlan: the parent device.
		InterfaceName?: string
		// The subnet in CIDR notation.
		Subnet?: [...#CIDR]
		// Define a gateway for the subnet. Requires a Subnet option.
		Gateway?: [...#IPAddress]
		// Allocate container IP from a range. Accepts CIDR or startIP-endIP syntax.
		IPRange?: [...#IPRange]
		// Enable IPv6 (Dual Stack) networking.
		IPv6?: bool
		// Restrict external access of this network.
		Internal?: bool
		// If enabled, disables the DNS plugin for this network.
		DisableDNS?: bool
		// When true, the network is deleted when the service is stopped.
		NetworkDeleteOnStop?: bool
		// Set network-scoped DNS resolver/nameserver for containers in this network.
		DNS?: [...#IPAddress]
		// Set one or more OCI labels on the network. Format: key=value.
		Label?: [...#KeyValue]
		// Arguments passed directly between "podman" and "network" for unsupported features.
		GlobalArgs?: [...string]
		// Arguments passed directly to the end of the podman network create command.
		PodmanArgs?: [...string]
		// Load the specified containers.conf(5) module.
		ContainersConfModule?: [...string]
	}
}
