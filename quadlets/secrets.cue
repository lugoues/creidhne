package quadlets

// #SecretName identifies a podman secret by name. Consumption details
// (type, target, mode) are added by the container that uses it.
#SecretName: {
	name: string & !=""
	...
}

// #SecretRegistry is a named set of podman secrets available on the host.
//
// Usage (in a secrets.cue):
//   secrets: quadlets.#SecretRegistry & {
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
#SecretRegistry: [Key=string]: #SecretName & {
	name: *Key | string
}
