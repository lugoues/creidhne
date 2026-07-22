package creidhne

// #ImageRegistry is a named set of managed image pins — the crei-owned source
// of truth for external images, living in registries/images.cue. Each entry
// separates the tracked reference (image, the channel you write) from the
// pinned digest (managed by `crei image pin`); the computed #ref combines
// them, so a container pulls by the digest (reproducible) while crei checks
// the tag for updates:
//
//   images: creidhne.#ImageRegistry & {
//       gluetun:   {image: "docker.io/qmcgaw/gluetun:v3", digest: "sha256:ad6b…"}
//       gotenberg: image: "docker.io/gotenberg/gotenberg:8.25"  // unpinned until pin
//       homeassistant: {image: "ghcr.io/home-assistant/home-assistant:stable", digest: "sha256:…", minAge: "3d"}
//   }
//
// Reference an entry's #ref from a container's Image:
//
//   Container: Image: images.gluetun.#ref
//
// image and digest are validated only loosely here; the crei image commands
// parse image with the full OCI grammar in Go, and classify each entry as
// managed (tag + digest), unpinned (tag, no digest), or unmanaged (digest,
// no trackable tag) for the image/unmanaged lint.
#ImageRegistry: [Key=string]: #ImageEntry

#ImageEntry: {
	// image is the tracked reference the entry follows: registry/repo[:tag],
	// without a digest (that is `digest`, managed separately). This is the
	// channel you hand-edit; crei image pin never rewrites it.
	image: #ImageRef & !~"@"

	// digest is the pinned content digest, written by crei image pin. Absent
	// or empty ("") means unpinned (not reproducible) until the next pin — a
	// new entry can be written either way; crei image pin fills it in.
	digest?: "" | =~"^sha256:[0-9a-f]+$"

	// minAge skips a candidate digest whose image is younger than this: an
	// integer with a d/w/h suffix ("7d", "2w", "12h"). See crei image outdated.
	minAge?: =~"^[0-9]+[dwh]$"

	// _digest is the effective pin: the digest when set, else "" (covering
	// both an omitted and an explicitly-empty field uniformly).
	_digest: [if digest != _|_ {digest}, ""][0]

	// #ref is what a container consumes: image@digest when pinned, else the
	// bare image. Computed, so it earns the hidden-handle convention (like
	// #self/#ref/#service): the consuming container reads #ref, never the
	// source fields.
	#ref: [if _digest != "" {"\(image)@\(_digest)"}, image][0]

	// Open for policy fields added in later phases (semver range, ...).
	...
}
