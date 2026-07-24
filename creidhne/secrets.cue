package creidhne

// #SecretName identifies a podman secret by name. Consumption details
// (type, target, mode) are added by the container that uses it.
#SecretName: {
	name: string & !=""
	...
}

// #SecretGenerate is the policy crei uses to synthesize a secret's value in
// `crei secret create` and `crei secret rotate`. It is metadata only — the
// generated material never lives in CUE; crei writes it into podman.
#SecretGenerate: {
	// length is the number of characters in the generated value.
	length: int & >=8 | *32
	// charset selects the alphabet: alphanumeric (default), hex, or base64.
	charset: *"alphanumeric" | "hex" | "base64"
}

// #SecretRegistry is a named set of podman secrets available on the host.
//
// Usage (in a secrets.cue):
//   secrets: creidhne.#SecretRegistry & {
//       soulseek_username: _
//       soulseek_password: _
//       tls_cert: { name: "tls-cert" }
//   }
//
// Then reference in your quadlet, adding consumption details:
//   Container: Secret: [
//       secrets.soulseek_username & { type: "env", target: "SLSKD_SLSK_USERNAME" },
//       secrets.tls_cert & { type: "mount", target: "/etc/ssl/cert.pem", mode: "0400" },
//   ]
//
// The container's Secret field (#SecretEntry) accepts both raw strings and
// #SecretRef structs. Unifying a registry entry with consumption fields
// produces a valid #SecretRef automatically.
//
// An entry may also carry a `generate` policy, which crei owns and writes into
// registries/secrets.cue; it tells `crei secret create`/`rotate` how to make
// the value and is ignored when the entry is consumed by a container:
//   secrets: creidhne.#SecretRegistry & {
//       db_password: generate: {length: 40}
//   }
#SecretRegistry: [Key=string]: #SecretName & {
	name: *Key | string
	generate?: #SecretGenerate
}
