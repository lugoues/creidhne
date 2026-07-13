package creidhne

import "list"

#Build: {
	name: string
	// _stem is injected by #Units; identity is computed inline from it.
	_stem:    string
	#ref:     "\(_stem).build"
	#service: "\(_stem)-build.service"

	// #self: reference handle (e.g. Image= built by this .build).
	#self: #RefSelf & {_kind: "build", source: #ref}

	Unit?:    #UnitSection
	Service?: #ServiceSection
	Install?: #InstallSection
	Quadlet?: #QuadletSection

	({
		// Inline Containerfile content. The Containerfile is emitted to
		// images/{stem}.Containerfile. File and SetWorkingDirectory are
		// injected by the renderer.
		ContainerFile: string
		// Optional build context directory. Entries are emitted under
		// images/{stem}.context/ as relative paths. When set,
		// SetWorkingDirectory points to the context directory.
		// Values can be plain strings (default mode "0644") or
		// structured with explicit mode (e.g. "0755" for scripts).
		Context?: [string]: string | {
			content: string
			mode:    #FileMode | *"0644"
		}
	} | {
		Build: {
			// Provide context (a working directory) to podman build via path, URL, or special keys.
			SetWorkingDirectory?: string
			// Specifies a Containerfile which contains instructions for building the image.
			File?: string
		}
	}) & {
		Build: {
			// Specifies the name(s) assigned to the resulting image if the build completes successfully. (optional: default "quadlets.localhost/\(_stem):latest")
			ImageTag: *["quadlets.localhost/\(_stem):latest"] | [...(string | [...string])]
			// Override the default systemd service unit name.
			ServiceName?: string
			// Path to an alternate .containerignore file to use when building the image.
			IgnoreFile?: string
			// Set the target build stage to build. Commands after the target stage are skipped.
			Target?: string
			// Specifies build arguments and their values, like environment variables.
			BuildArg?: [...(#KeyValue | [...#KeyValue])]
			// Add a value (e.g. env=value) to the built image using systemd format.
			Environment?: [...(#KeyValue | [...#KeyValue])]
			// Override the architecture (defaults to host's) of the image to be built.
			Arch?: string
			// Override the default architecture variant of the container image to be built.
			Variant?: string
			// Path of the authentication file.
			AuthFile?: string
			// Configure network namespace for RUN instructions during build.
			// A raw mode (#NetworkMode) or a network #self. (Strict.)
			Network?: [...((#NetworkMode | #NetworkSelf) | [...(#NetworkMode | #NetworkSelf)])]
			// Set network-scoped DNS resolver/nameserver for the build container.
			DNS?: [...(#IPAddress | [...#IPAddress])]
			// Set custom DNS options.
			DNSOption?: [...(string | [...string])]
			// Set custom DNS search domains. Use DNSSearch=. to remove the search domain.
			DNSSearch?: [...(string | [...string])]
			// Add an image label (e.g. label=value) to the image metadata. A raw
			// "key=value" string, or a #Rendered helper (e.g. #JSONLabel).
			Label?: [...(#LabelValue | [...#LabelValue])]
			// Add an image annotation (e.g. annotation=value) to the image metadata.
			Annotation?: [...(#KeyValue | [...#KeyValue])]
			// Always remove intermediate containers after a build, even if the build fails.
			ForceRM?: bool
			// Set the image pull policy.
			Pull?: #PullPolicy
			// Require HTTPS and verification of certificates when contacting registries.
			TLSVerify?: bool
			// Pass secret information used in Containerfile build stages in a safe way.
			Secret?: [...(string | [...string])]
			// Mount a volume to containers when executing RUN instructions during the
			// build. A host mount (#HostMount) or a volume #self. (Strict.)
			Volume?: [...((#HostMount | #VolumeMountRef) | [...(#HostMount | #VolumeMountRef)])]
			// Assign additional groups to the primary user running within the container process.
			GroupAdd?: [...(string | [...string])]
			// Number of times to retry the image pull when an HTTP error occurs.
			Retry?: int & >=0
			// Delay between retries.
			RetryDelay?: #GoDuration
			// Arguments passed directly between "podman" and "build" for unsupported features.
			GlobalArgs?: [...(string | [...string])]
			// Arguments passed directly to the end of the podman build command.
			PodmanArgs?: [...(string | [...string])]
			// Load the specified containers.conf(5) module.
			ContainersConfModule?: [...(string | [...string])]

			// Resolved networks/volumes: flatten #self refs to strings.
			networkStrings: list.Concat([
				if Network != _|_ for e in Network {
					[
						if (e & [...]) == _|_ {(e & string) | (e & {#rendered: _}).#rendered},
						if (e & [...]) != _|_ for n in (e & [...]) {(n & string) | (n & {#rendered: _}).#rendered},
					]
				},
			])
			volumeStrings: list.Concat([
				if Volume != _|_ for e in Volume {
					[
						if (e & [...]) == _|_ {(e & string) | (e & {#rendered: _}).#rendered},
						if (e & [...]) != _|_ for v in (e & [...]) {(v & string) | (v & {#rendered: _}).#rendered},
					]
				},
			])
			labelStrings: list.Concat([
				if Label != _|_ for e in Label {
					list.Concat([
						if (e & [...]) == _|_ {(_#renderLabel & {#e: e}).out},
						if (e & [...]) != _|_ for l in (e & [...]) {(_#renderLabel & {#e: l}).out},
					])
				},
			])
		}
	}
}
