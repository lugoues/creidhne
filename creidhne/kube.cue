package creidhne

#Kube: {
	name:     string
	// _stem is injected by #Units; identity is computed inline from it.
	_stem:    string
	#ref:     "\(_stem).kube"
	#service: "\(_stem).service"
	// #self: reference handle.
	#self: #RefSelf & {_kind: "kube", source: #ref}

	Unit?:    #UnitSection
	Service?: #ServiceSection
	Install?: #InstallSection
	Quadlet?: #QuadletSection

	Kube: {
		// The path (absolute or relative to the unit file) to the Kubernetes YAML file.
		Yaml: [...(string | [...string])] & [_, ...]
		// Override the default systemd service unit name.
		ServiceName?: string
		// Pass Kubernetes ConfigMap YAML paths to podman kube play via --configmap.
		ConfigMap?: [...(string | [...string])]
		// Indicates whether containers will be auto-updated (registry or local).
		AutoUpdate?: [...(string | [...string])]
		// Control main PID exit behavior: all (all fail), any (any fail), or none (ignore failures).
		ExitCodePropagation?: #ExitCodePropagation
		// Remove all resources, including volumes, when calling podman kube down.
		KubeDownForce?: bool
		// Set the log-driver Podman uses when running the container.
		LogDriver?: #LogDriver
		// Set the logging options used by Podman when running the container.
		LogOpt?: [...(string | [...string])]
		// Specify a custom network for the container. Supports .network Quadlet file references.
		Network?: [...(string | [...string])]
		// Exposes a port, or a range of ports, from the container to the host.
		PublishPort?: [...(#PortMapping | [...#PortMapping])]
		// Set the user namespace mode for the container.
		UserNS?: string
		// Set the WorkingDirectory field to "yaml" or "unit" file location for relative path resolution.
		SetWorkingDirectory?: "yaml" | "unit"
		// Arguments passed directly between "podman" and "kube" for unsupported features.
		GlobalArgs?: [...(string | [...string])]
		// Arguments passed directly to the end of the podman kube play command.
		PodmanArgs?: [...(string | [...string])]
		// Load the specified containers.conf(5) module.
		ContainersConfModule?: [...(string | [...string])]
	}
}
