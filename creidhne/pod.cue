package creidhne

import "list"

#Pod: {
	name: string
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

		// Specify a custom network for the pod. Accepts a raw mode (#NetworkMode)
		// or a network/container #self handle. (Strict: named refs go through #self.)
		Network?: [...((#NetworkMode | #NetworkSelf | #ContainerSelf) | [...(#NetworkMode | #NetworkSelf | #ContainerSelf)])]
		// Add a network-scoped alias for the pod for DNS resolution grouping.
		NetworkAlias?: [...(string | [...string])]
		// Exposes a port, or a range of ports, from the pod to the host.
		PublishPort?: [...(#PortMapping | [...#PortMapping])]
		// Set the pod's hostname inside all containers.
		HostName?: string
		// Specify a static IPv4 address for the pod.
		IP?: #IPv4
		// Specify a static IPv6 address for the pod.
		IP6?: #IPv6
		// Set network-scoped DNS resolver/nameserver for containers in this pod.
		DNS?: [...(#IPAddress | [...#IPAddress])]
		// Set custom DNS options.
		DNSOption?: [...(string | [...string])]
		// Set custom DNS search domains. Use DNSSearch=. to remove the search domain.
		DNSSearch?: [...(string | [...string])]
		// Add host-to-IP mapping to /etc/hosts. Format: hostname:ip.
		AddHost?: [...(#HostMapping | [...#HostMapping])]

		// Mount a volume in the pod. Accepts a host bind/anonymous mount or a
		// managed/external volume via its #self handle. (Strict: no bare names.)
		Volume?: [...((#HostMount | #VolumeMountRef) | [...(#HostMount | #VolumeMountRef)])]
		// Size of /dev/shm.
		ShmSize?: #PodmanBytes

		// Set one or more OCI labels on the pod. A raw "key=value" string, or a
		// #Rendered helper (e.g. #JSONLabel) that computes one.
		Label?: [...(#LabelValue | [...#LabelValue])]

		// Set the user namespace mode for the pod.
		UserNS?: #UserNSEntry
		// Create the pod in a new user namespace using the supplied UID mapping.
		UIDMap?: [...(#IDMap | [...#IDMap])]
		// Create the pod in a new user namespace using the supplied GID mapping.
		GIDMap?: [...(#IDMap | [...#IDMap])]
		// Use the named UID map from /etc/subuid for the pod namespace.
		SubUIDMap?: string
		// Use the named GID map from /etc/subgid for the pod namespace.
		SubGIDMap?: string

		// Set the exit policy of the pod when the last container exits. Default for quadlets is "stop".
		ExitPolicy?: #PodExitPolicy
		// Seconds to wait for the pod to gracefully stop.
		StopTimeout?: int & >=0

		// Arguments passed directly between "podman" and "pod" for unsupported features.
		GlobalArgs?: [...(string | [...string])]
		// Arguments passed directly to the end of the podman pod create command.
		PodmanArgs?: [...(string | [...string])]

		// Load the specified containers.conf(5) module.
		ContainersConfModule?: [...(string | [...string])]
	}

	// Resolved volumes: flattens #self volume refs to strings for the template.
	volumeStrings: list.Concat([
		if Pod.Volume != _|_ for e in Pod.Volume {
			[
				if (e & [...]) == _|_ {(e & string) | (e & {#rendered: _}).#rendered},
				if (e & [...]) != _|_ for v in (e & [...]) {(v & string) | (v & {#rendered: _}).#rendered},
			]
		},
	])

	// Resolved networks: flattens #self network/container refs for the template.
	networkStrings: list.Concat([
		if Pod.Network != _|_ for e in Pod.Network {
			[
				if (e & [...]) == _|_ {(e & string) | (e & {#rendered: _}).#rendered},
				if (e & [...]) != _|_ for n in (e & [...]) {(n & string) | (n & {#rendered: _}).#rendered},
			]
		},
	])

	// Resolved labels: raw strings pass through; #Rendered helpers contribute
	// their #rendered, one label or a spliced list (_#renderLabel).
	labelStrings: list.Concat([
		if Pod.Label != _|_ for e in Pod.Label {
			list.Concat([
				if (e & [...]) == _|_ {(_#renderLabel & {#e: e}).out},
				if (e & [...]) != _|_ for l in (e & [...]) {(_#renderLabel & {#e: l}).out},
			])
		},
	])

	// Resolved user namespace (scalar); present only when set.
	if Pod.UserNS != _|_ {
		userNSString: (Pod.UserNS & string) | (Pod.UserNS & {#rendered: _}).#rendered
	}
}
