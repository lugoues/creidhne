package creidhne

#Volume: {
	name: string
	// _stem is injected by #Units; identity is computed inline from it.
	_stem:    string
	#ref:     "\(_stem).volume"
	#service: "\(_stem)-volume.service"

	// #self: reference handle for a Volume= field, e.g.
	//   Volume: [units.volumes.data.#self & {target: "/data", options: "U"}]
	#self: #VolumeSelf & {source: #ref}

	// #volumeName resolves to the explicit VolumeName, else systemd-%N.
	#volumeName: *Volume.VolumeName | "systemd-\(_stem)"

	Unit?:    #UnitSection
	Service?: #ServiceSection
	Install?: #InstallSection
	Quadlet?: #QuadletSection

	Volume?: {
		// Override the default volume name. Defaults to systemd-%N.
		VolumeName?: string
		// Override the default systemd service unit name.
		ServiceName?: string
		// Specify the volume driver name. When set to "image", the Image key must also be set.
		Driver?: string
		// The mount options to use for a filesystem, per mount(8) -o option.
		Options?: [...string]
		// The filesystem type of Device, per mount(8) -t option.
		Type?: string
		// The path of a device which is mounted for the volume.
		Device?: [...string]
		// If enabled, the content of the image at the mountpoint is copied into the volume on first run.
		Copy: *true | bool
		// Specifies the image the volume is based on when Driver is set to "image".
		Image?: #ImageRef
		// The host (numeric) UID, or user name to use as the owner for the volume.
		User?: string
		// The host (numeric) GID, or group name to use as the group for the volume.
		Group?: string
		// The host numeric UID to use as the owner for the volume.
		UID?: int & >=0
		// The host numeric GID to use as the group for the volume.
		GID?: int & >=0
		// Set one or more OCI labels on the volume. A raw "key=value" string, or a
		// #Rendered helper (e.g. #JSONLabel) that computes one.
		Label?: [...#LabelValue]
		// Arguments passed directly between "podman" and "volume" for unsupported features.
		GlobalArgs?: [...string]
		// Arguments passed directly to the end of the podman volume create command.
		PodmanArgs?: [...string]
		// Load the specified containers.conf(5) module.
		ContainersConfModule?: [...string]
	}

	// Resolved labels: raw strings pass through; #Rendered helpers flatten.
	labelStrings: [
		if Volume.Label != _|_ for l in Volume.Label {
			(l & string) | (l & {_rendered: _})._rendered
		},
	]
}
