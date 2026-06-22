package creidhne

#Image: {
	name:     string
	// _stem is injected by #Units; identity is computed inline from it.
	_stem:    string
	#ref:     "\(_stem).image"
	#service: "\(_stem)-image.service"
	// #self: reference handle (e.g. Image= pulled by this .image).
	#self: #RefSelf & {_kind: "image", source: #ref}

	Unit?:    #UnitSection
	Service?: #ServiceSection
	Install?: #InstallSection
	Quadlet?: #QuadletSection

	Image: {
		// The image to pull. It is recommended to use a fully qualified image name.
		Image: #ImageRef
		// Override the default systemd service unit name.
		ServiceName?: string
		// All tagged images in the repository are pulled.
		AllTags?: bool
		// The pull policy to use when pulling the image.
		Policy?: #PullPolicy
		// Override the architecture (defaults to host's) of the image to be pulled.
		Arch?: string
		// Override the OS (defaults to host's) of the image to be pulled.
		OS?: string
		// Override the default architecture variant of the container image.
		Variant?: string
		// Path of the authentication file.
		AuthFile?: string
		// Use certificates at path (*.crt, *.cert, *.key) to connect to the registry.
		CertDir?: string
		// The [username[:password]] to use to authenticate with the registry.
		Creds?: string
		// The [key[:passphrase]] to be used for decryption of images.
		DecryptionKey?: string
		// Require HTTPS and verification of certificates when contacting registries.
		TLSVerify?: bool
		// Actual FQIN reference when source is a file or directory archive.
		ImageTag?: string
		// Number of times to retry the image pull when an HTTP error occurs.
		Retry?: int & >=0
		// Delay between retries.
		RetryDelay?: #GoDuration
		// Arguments passed directly between "podman" and "image" for unsupported features.
		GlobalArgs?: [...string]
		// Arguments passed directly to the end of the podman image pull command.
		PodmanArgs?: [...string]
		// Load the specified containers.conf(5) module.
		ContainersConfModule?: [...string]
	}
}
