package creidhne

// #ImageRegistry is a named set of managed image pins — the crei-owned source
// of truth for external images, living in registries/images.cue. Each entry
// tracks a tag and pins a digest; `crei image pin`/`outdated` resolve the tag
// and manage the digest. The managed ref form carries both, so podman pulls by
// the digest (reproducible) while crei checks the tag for updates:
//
//   images: creidhne.#ImageRegistry & {
//       gluetun:   ref: "docker.io/qmcgaw/gluetun:v3@sha256:ad6b…"
//       gotenberg: ref: "docker.io/gotenberg/gotenberg:8.25@sha256:…"
//       homeassistant: {ref: "ghcr.io/home-assistant/home-assistant:stable@sha256:…", minAge: "3d"}
//   }
//
// Reference an entry's ref from a container's Image:
//
//   Container: Image: images.gluetun.ref
//
// The ref is validated only loosely here (a non-empty, space-free string); the
// crei image commands parse it with the full OCI grammar and classify each
// entry as managed (tag + digest), unpinned (tag, no digest), or unmanaged
// (digest, no tag) for the image/unmanaged lint.
#ImageRegistry: [Key=string]: #ImageEntry

#ImageEntry: {
	// ref is the OCI reference the consuming container uses. Validated only
	// loosely (#ImageRef: non-empty); the crei image commands parse the full
	// grammar (registry/repo:tag@digest) in Go where a real OCI parser
	// handles the edge cases.
	ref: #ImageRef

	// minAge skips a candidate digest whose image is younger than this,
	// overriding the global default from crei.toml. See crei image outdated.
	minAge?: #GoDuration

	// Open for policy fields added in later phases (semver range, ...).
	...
}
