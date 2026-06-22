package creidhne

#Container: {
	name:     string
	// _stem is injected by #Units; identity is computed inline from it.
	_stem:    string
	#ref:     "\(_stem).container"
	#service: "\(_stem).service"
	// #self: reference handle (e.g. for Network=container:... reuse via .container).
	#self: #RefSelf & {_kind: "container", source: #ref}

	// #containerName is the resolved ContainerName: the explicit value if set,
	// else podman's systemd-%N default. Reference it from other units, e.g.
	// Network: ["container:\(db.units.#container.#containerName)"].
	#containerName: *Container.ContainerName | "systemd-\(_stem)"

	Unit?:    #UnitSection
	Service?: #ServiceSection
	Install?: #InstallSection
	Quadlet?: #QuadletSection

	Container: {Image: #ImageRef} | {Rootfs: string}
	Container: {ReloadCmd: string} | {ReloadSignal: #Signal} | *{}
	Container: {

		// The (optional) name of the Podman container. If not specified, the default value is systemd-%N.
		ContainerName?: string
		// Override the default systemd service unit name.
		ServiceName?: string

		// Override the default ENTRYPOINT from the image.
		Entrypoint?: string
		// Additional container arguments matching systemd command line format.
		Exec?: string
		// Working directory inside the container. Overrides the image's default directory.
		WorkingDir?: string
		// The (numeric) UID to run as inside the container.
		User?: string
		// The (numeric) GID to run as inside the container.
		Group?: string
		// If enabled, the container has a minimal init process inside that forwards signals and reaps processes.
		RunInit?: bool

		// Set an environment variable in the container. Uses the same format as services in systemd.
		Environment?: [...#KeyValue]
		// Use a line-delimited file to set environment variables in the container.
		EnvironmentFile?: [...string]
		// Use the host environment inside of the container.
		EnvironmentHost?: bool

		// Specify a custom network for the container. Accepts raw modes (host,
		// none, bridge, container:NAME, name.network, ...) or a network/container
		// #self handle: units.networks.X.#self.
		Network?: [...(string | #NetworkSelf | #ContainerSelf)]
		// Add a network-scoped alias for the container. Aliases can group containers in DNS resolution.
		NetworkAlias?: [...string]
		// Exposes a port, or a range of ports, from the container to the host.
		PublishPort?: [...#PortMapping]
		// Exposes a port, or a range of ports, from the host to the container.
		ExposeHostPort?: [...string]
		// Sets the host name that is available inside the container.
		HostName?: string
		// Specify a static IPv4 address for the container.
		IP?: #IPv4
		// Specify a static IPv6 address for the container.
		IP6?: #IPv6
		// Set network-scoped DNS resolver/nameserver for the container.
		DNS?: [...#IPAddress]
		// Set custom DNS options.
		DNSOption?: [...string]
		// Set custom DNS search domains. Use DNSSearch=. to remove the search domain.
		DNSSearch?: [...string]
		// Add host-to-IP mapping to /etc/hosts. Format: hostname:ip.
		AddHost?: [...#HostMapping]
		// Controls whether proxy environment variables pass from Podman into the container.
		HttpProxy?: bool

		// Mount a volume in the container. Accepts a raw string mount or a managed/
		// external volume's #self handle: units.volumes.X.#self & {target: "/path"}.
		Volume?: [...(#VolumeMount | #VolumeMountRef)]
		// Attach a filesystem mount to the container.
		Mount?: [...string]
		// Mount a tmpfs in the container.
		Tmpfs?: [...string]

		// Add these capabilities, in addition to the default Podman capability set, to the container.
		AddCapability?: [...string]
		// Remove capabilities from the default Podman set. Use "ALL" to drop everything.
		DropCapability?: [...string]
		// If enabled, disables the container processes from gaining additional privileges.
		NoNewPrivileges?: bool
		// Set the seccomp profile for the container. Use "unconfined" to disable filters.
		SeccompProfile?: string
		// Set the apparmor confinement profile for the container. Use "unconfined" to disable.
		AppArmor?: string
		// Turn off label separation for the container.
		SecurityLabelDisable?: bool
		// Set the label file type for the container files.
		SecurityLabelFileType?: string
		// Set the label process level for the container processes.
		SecurityLabelLevel?: string
		// Allow SecurityLabels to function within the container.
		SecurityLabelNested?: bool
		// Set the label process type for the container processes.
		SecurityLabelType?: string
		// Specify paths to mask (colon-separated). Masked paths cannot be accessed inside the container.
		Mask?: string
		// Specify paths to unmask. Can be "ALL" or a colon-separated path list.
		Unmask?: string
		// If enabled, makes the image read-only.
		ReadOnly?: bool
		// If ReadOnly is true, mount a read-write tmpfs on /dev, /dev/shm, /run, /tmp, /var/tmp.
		ReadOnlyTmpfs?: bool

		// Add device nodes from the host into the container.
		AddDevice?: [...#DeviceMapping]

		// Memory limit for the container.
		Memory?: #PodmanBytes
		// Tune the container's pids limit.
		PidsLimit?: -1 | (int & >0)
		// Ulimit options. Sets the ulimits values inside of the container.
		Ulimit?: [...string]
		// Size of /dev/shm.
		ShmSize?: #PodmanBytes

		// Set one or more OCI labels on the container. Format: key=value.
		Label?: [...#KeyValue]
		// Set one or more OCI annotations on the container. Format: key=value.
		Annotation?: [...#KeyValue]

		// Set or alter a healthcheck command for the container. Use "none" to disable.
		HealthCmd?: string
		// Set an interval for the healthchecks. Use "disable" for no automatic timer.
		HealthInterval?: #GoDuration
		// The number of retries allowed before a healthcheck is considered unhealthy.
		HealthRetries?: int & >=0
		// The initialization time needed for a container to bootstrap.
		HealthStartPeriod?: #GoDuration
		// The maximum time allowed to complete the healthcheck before it is marked failed.
		HealthTimeout?: #GoDuration
		// Action to take once the container transitions to an unhealthy state.
		HealthOnFailure?: #HealthOnFailure
		// Set the destination for HealthCheck logs: local, directory path, or events_logger.
		HealthLogDestination?: string
		// Set maximum number of attempts in the HealthCheck log file. 0 means infinite.
		HealthMaxLogCount?: int & >=0
		// Set maximum length in characters of stored HealthCheck log. 0 means infinite.
		HealthMaxLogSize?: #PodmanBytes
		// Set a startup healthcheck command for the container.
		HealthStartupCmd?: string
		// Set an interval for the startup healthcheck.
		HealthStartupInterval?: #GoDuration
		// The number of attempts allowed before the startup healthcheck restarts the container.
		HealthStartupRetries?: int & >=0
		// The number of successful runs required before the startup healthcheck succeeds.
		HealthStartupSuccess?: int & >=0
		// The maximum time a startup healthcheck command has to complete before it is marked failed.
		HealthStartupTimeout?: #GoDuration

		// Set the log-driver used by Podman when running the container.
		LogDriver?: #LogDriver
		// Set the logging options used by Podman when running the container.
		LogOpt?: [...string]

		// Signal to stop a container. Default is SIGTERM.
		StopSignal?: #Signal
		// Seconds to wait before forcibly stopping the container.
		StopTimeout?: int & >=0
		// Controls sd_notify startup behavior. Can be false, true, or "healthy".
		Notify?: #NotifyMode
		// Specifies the cgroup mode for the Podman container.
		CgroupsMode?: #CgroupsMode

		// Set the user namespace mode for the container.
		UserNS?: #UserNS
		// Run the container in a new user namespace using the supplied UID mapping.
		UIDMap?: [...#IDMap]
		// Run the container in a new user namespace using the supplied GID mapping.
		GIDMap?: [...#IDMap]
		// Run the container in a new user namespace using the map with name in /etc/subuid.
		SubUIDMap?: string
		// Run the container in a new user namespace using the map with name in /etc/subgid.
		SubGIDMap?: string
		// Assign additional groups to the primary user running within the container process.
		GroupAdd?: [...string]

		// Link the container to a Quadlet .pod unit via its #self handle:
		// Pod: units.#pod.#self. (Strict: Pod= is ref-only, no raw values.)
		Pod?: #PodSelf
		// Determines whether the container starts with the associated pod. Defaults to true.
		StartWithPod?: bool

		// Indicates whether the container will be auto-updated (registry or local).
		AutoUpdate?: #AutoUpdatePolicy
		// Set the image pull policy.
		Pull?: #PullPolicy
		// Number of times to retry the image pull when an HTTP error occurs.
		Retry?: int & >=0
		// Delay between retries.
		RetryDelay?: #GoDuration

		// Use a Podman secret in the container either as a file or an environment variable.
		// Accepts raw strings or structured #SecretRef objects.
		Secret?: [...#SecretEntry]

		// Configures namespaced kernel parameters for the container. Format: name=value.
		Sysctl?: [...string]

		// The timezone to run the container in.
		Timezone?: string

		// Arguments passed directly between "podman" and "run" for unsupported features.
		GlobalArgs?: [...string]
		// Arguments passed directly to the end of the podman run command.
		PodmanArgs?: [...string]

		// Load the specified containers.conf(5) module.
		ContainersConfModule?: [...string]
	}

	// Resolved secrets: flattens #SecretRef structs to strings for template rendering.
	secretStrings: [
		if Container.Secret != _|_ for s in Container.Secret {
			(s & string) | (s & {_rendered: _})._rendered
		},
	]

	// Resolved volumes: flattens #self volume refs to "source:target[:options]"
	// strings; raw string mounts pass through unchanged. Same mechanism as
	// secretStrings, consumed by the template instead of Container.Volume.
	volumeStrings: [
		if Container.Volume != _|_ for v in Container.Volume {
			(v & string) | (v & {_rendered: _})._rendered
		},
	]

	// Resolved networks: flattens #self network/container refs to their ref
	// string; raw modes pass through unchanged.
	networkStrings: [
		if Container.Network != _|_ for n in Container.Network {
			(n & string) | (n & {_rendered: _})._rendered
		},
	]

	// Resolved pod (scalar): a #self pod ref flattens to its .pod ref; a raw
	// string passes through. Only present when Pod is set.
	if Container.Pod != _|_ {
		podString: (Container.Pod & string) | (Container.Pod & {_rendered: _})._rendered
	}
}
