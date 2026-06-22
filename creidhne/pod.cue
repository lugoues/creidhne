package creidhne

#Pod: {
	name:     string
	// _stem is injected by #Units; identity is computed inline from it.
	_stem:    string
	#ref:     "\(_stem).pod"
	#service: "\(_stem)-pod.service"
	// #self: reference handle for a Pod= field.
	#self: #RefSelf & {_kind: "pod", source: #ref}

	// #podName resolves to the explicit PodName, else systemd-%N.
	#podName: *Pod.PodName | "systemd-\(_stem)"

	Unit?:    #UnitSection
	Service?: #ServiceSection
	Install?: #InstallSection
	Quadlet?: #QuadletSection

	Pod?: {
		// Override the default pod name. Defaults to systemd-%N.
		PodName?: string
		// Override the default systemd service unit name.
		ServiceName?: string

		// Specify a custom network for the pod. Accepts raw modes or a
		// network/container #self handle.
		Network?: [...(string | #NetworkSelf | #ContainerSelf)]
		// Add a network-scoped alias for the pod for DNS resolution grouping.
		NetworkAlias?: [...string]
		// Exposes a port, or a range of ports, from the pod to the host.
		PublishPort?: [...#PortMapping]
		// Set the pod's hostname inside all containers.
		HostName?: string
		// Specify a static IPv4 address for the pod.
		IP?: #IPv4
		// Specify a static IPv6 address for the pod.
		IP6?: #IPv6
		// Set network-scoped DNS resolver/nameserver for containers in this pod.
		DNS?: [...#IPAddress]
		// Set custom DNS options.
		DNSOption?: [...string]
		// Set custom DNS search domains. Use DNSSearch=. to remove the search domain.
		DNSSearch?: [...string]
		// Add host-to-IP mapping to /etc/hosts. Format: hostname:ip.
		AddHost?: [...#HostMapping]

		// Mount a volume in the pod. Accepts a raw string mount or a managed/
		// external volume's #self handle: units.volumes.X.#self & {target: "/path"}.
		Volume?: [...(#VolumeMount | #VolumeMountRef)]
		// Size of /dev/shm.
		ShmSize?: #PodmanBytes

		// Set one or more OCI labels on the pod. Format: key=value.
		Label?: [...#KeyValue]

		// Set the user namespace mode for the pod.
		UserNS?: #UserNS
		// Create the pod in a new user namespace using the supplied UID mapping.
		UIDMap?: [...#IDMap]
		// Create the pod in a new user namespace using the supplied GID mapping.
		GIDMap?: [...#IDMap]
		// Use the named UID map from /etc/subuid for the pod namespace.
		SubUIDMap?: string
		// Use the named GID map from /etc/subgid for the pod namespace.
		SubGIDMap?: string

		// Set the exit policy of the pod when the last container exits. Default for quadlets is "stop".
		ExitPolicy?: #PodExitPolicy
		// Seconds to wait for the pod to gracefully stop.
		StopTimeout?: int & >=0

		// Arguments passed directly between "podman" and "pod" for unsupported features.
		GlobalArgs?: [...string]
		// Arguments passed directly to the end of the podman pod create command.
		PodmanArgs?: [...string]

		// Load the specified containers.conf(5) module.
		ContainersConfModule?: [...string]
	}

	// Resolved volumes: flattens #self volume refs to strings for the template.
	volumeStrings: [
		if Pod.Volume != _|_ for v in Pod.Volume {
			(v & string) | (v & {_rendered: _})._rendered
		},
	]

	// Resolved networks: flattens #self network/container refs for the template.
	networkStrings: [
		if Pod.Network != _|_ for n in Pod.Network {
			(n & string) | (n & {_rendered: _})._rendered
		},
	]
}
