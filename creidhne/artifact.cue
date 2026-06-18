package creidhne

#Artifact: {
	// #stem is injected by #Units; identity is computed inline from it.
	#stem:    string
	#ref:     "\(#stem).artifact"
	#service: "\(#stem)-artifact.service"

	Unit?:    #UnitSection
	Service?: #ServiceSection
	Install?: #InstallSection
	Quadlet?: #QuadletSection

	Artifact: {
		// The artifact to pull from a registry onto the local machine. Required.
		Artifact: string & !=""
		// Override the default systemd service unit name.
		ServiceName?: string
		// Path of the authentication file.
		AuthFile?: string
		// Use certificates at path (*.crt, *.cert, *.key) to connect to the registry.
		CertDir?: string
		// The credentials to use when contacting the registry. Format: username[:password].
		Creds?: string
		// The [key[:passphrase]] to be used for decryption of artifacts.
		DecryptionKey?: string
		// Require HTTPS and verification of certificates when contacting registries.
		TLSVerify?: bool
		// Suppress output information when pulling artifacts.
		Quiet?: bool
		// Number of times to retry the artifact pull when an HTTP error occurs.
		Retry?: int & >=0
		// Delay between retries.
		RetryDelay?: #GoDuration
		// Arguments passed directly between "podman" and "artifact" for unsupported features.
		GlobalArgs?: [...string]
		// Arguments passed directly to the end of the podman artifact pull command.
		PodmanArgs?: [...string]
		// Load the specified containers.conf(5) module.
		ContainersConfModule?: [...string]
	}
}
