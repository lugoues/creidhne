package creidhne

#Build: {
	#Reference
	#unitType:      "build"
	#serviceSuffix: "-build"

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
			// Specifies the name(s) assigned to the resulting image if the build completes successfully.
			ImageTag: [...string] & [_, ...]
			// Override the default systemd service unit name.
			ServiceName?: string
			// Path to an alternate .containerignore file to use when building the image.
			IgnoreFile?: string
			// Set the target build stage to build. Commands after the target stage are skipped.
			Target?: string
			// Specifies build arguments and their values, like environment variables.
			BuildArg?: [...#KeyValue]
			// Add a value (e.g. env=value) to the built image using systemd format.
			Environment?: [...#KeyValue]
			// Override the architecture (defaults to host's) of the image to be built.
			Arch?: string
			// Override the default architecture variant of the container image to be built.
			Variant?: string
			// Path of the authentication file.
			AuthFile?: string
			// Configure network namespace for RUN instructions during build.
			Network?: [...string]
			// Set network-scoped DNS resolver/nameserver for the build container.
			DNS?: [...#IPAddress]
			// Set custom DNS options.
			DNSOption?: [...string]
			// Set custom DNS search domains. Use DNSSearch=. to remove the search domain.
			DNSSearch?: [...string]
			// Add an image label (e.g. label=value) to the image metadata.
			Label?: [...#KeyValue]
			// Add an image annotation (e.g. annotation=value) to the image metadata.
			Annotation?: [...#KeyValue]
			// Always remove intermediate containers after a build, even if the build fails.
			ForceRM?: bool
			// Set the image pull policy.
			Pull?: #PullPolicy
			// Require HTTPS and verification of certificates when contacting registries.
			TLSVerify?: bool
			// Pass secret information used in Containerfile build stages in a safe way.
			Secret?: [...string]
			// Mount a volume to containers when executing RUN instructions during the build.
			Volume?: [...#VolumeMount]
			// Assign additional groups to the primary user running within the container process.
			GroupAdd?: [...string]
			// Number of times to retry the image pull when an HTTP error occurs.
			Retry?: int & >=0
			// Delay between retries.
			RetryDelay?: #GoDuration
			// Arguments passed directly between "podman" and "build" for unsupported features.
			GlobalArgs?: [...string]
			// Arguments passed directly to the end of the podman build command.
			PodmanArgs?: [...string]
			// Load the specified containers.conf(5) module.
			ContainersConfModule?: [...string]
		}
	}
}
