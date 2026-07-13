package creidhne

// #Units constrains the units map within a #Quadlet.
//
// Hash-prefixed fields (#container, #pod, #build, etc.) are the "primary"
// unit for this quadlet, named after the quadlet itself (e.g., traefik.container).
//
// Plural fields (containers, pods, builds, etc.) are additional units,
// keyed and prefixed (e.g., traefik-errors.container).
#Units: {
	#quadletName: string
	let _qn = #quadletName

	// Primary units (singular): name and stem are the quadlet name (not overridable).
	#container?: #Container & {name: _qn, _stem: _qn}
	#pod?: #Pod & {name: _qn, _stem: _qn}
	#volume?: #Volume & {name: _qn, _stem: _qn}
	#network?: #Network & {name: _qn, _stem: _qn}
	#kube?: #Kube & {name: _qn, _stem: _qn}
	#build?: #Build & {name: _qn, _stem: _qn}
	#image?: #Image & {name: _qn, _stem: _qn}
	#artifact?: #Artifact & {name: _qn, _stem: _qn}

	// Additional units (plural, keyed): stem is "<quadlet>-<name>" (name defaults
	// to the key). name is constrained to #UnitName, which also rejects an unsafe
	// map key (a key that isn't a valid name fails when it defaults into name).
	containers: [Key=string]: #Container & {name: #UnitName & (*Key | string), _stem: "\(_qn)-\(name)"}
	pods: [Key=string]: #Pod & {name: #UnitName & (*Key | string), _stem: "\(_qn)-\(name)"}
	volumes: [Key=string]: #Volume & {name: #UnitName & (*Key | string), _stem: "\(_qn)-\(name)"}
	networks: [Key=string]: #Network & {name: #UnitName & (*Key | string), _stem: "\(_qn)-\(name)"}
	kubes: [Key=string]: #Kube & {name: #UnitName & (*Key | string), _stem: "\(_qn)-\(name)"}
	builds: [Key=string]: #Build & {name: #UnitName & (*Key | string), _stem: "\(_qn)-\(name)"}
	images: [Key=string]: #Image & {name: #UnitName & (*Key | string), _stem: "\(_qn)-\(name)"}
	artifacts: [Key=string]: #Artifact & {name: #UnitName & (*Key | string), _stem: "\(_qn)-\(name)"}
}

// #Quadlet is a self-contained, named deployment unit.
//
// Usage:
//
//   import "github.com/lugoues/creidhne@v0"
//
//   // Define an external dependency registry (optional helper pattern):
//   externals: creidhne.#ExternalUnits & {
//       targets: "network-online": _
//       sockets: podman: _
//   }
//
//   traefik: creidhne.#Quadlet & {
//       name: "traefik"
//       units: {
//           #build: { Build: { ImageTag: ["localhost/traefik:quadlet"] } }
//           #container: {
//               Container: {
//                   Image: units.#build.#ref
//                   // Cross-quadlet reference:
//                   Volume: ["\(socket_proxy.units.#volume.#ref):/mnt/spx:ro"]
//               }
//               // External dep via registry:
//               Unit: After: [externals.targets["network-online"].#service]
//               // Or use raw strings:
//               // Unit: After: ["network-online.target"]
//           }
//           containers: errors: { Container: { Image: "docker.io/11notes/traefik:errors" } }
//           volumes: acme: {}
//       }
//   }
#Quadlet: {
	name: #UnitName
	units: #Units & {#quadletName: name}

	// manifest is the exported, non-hidden contract consumed by the Go
	// renderer. `cue export` drops hidden fields (#container, #ref, #service),
	// so each unit's computed stem/#ref/#service are *promoted* here into
	// regular fields alongside the unit value (data). The Go side reads this
	// list, dispatches on kind, and renders each unit's data through the
	// matching text/template. Primary units (singular, optional) are included
	// via a single-element comprehension guarded by `!= _|_`; additional units
	// come from the plural maps. Ordering is irrelevant. The renderer sorts by
	// filename.
	manifest: [
		for u in [units.#container] if u != _|_ {kind: "container", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.containers {kind: "container", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#pod] if u != _|_ {kind: "pod", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.pods {kind: "pod", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#volume] if u != _|_ {kind: "volume", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.volumes {kind: "volume", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#network] if u != _|_ {kind: "network", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.networks {kind: "network", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#kube] if u != _|_ {kind: "kube", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.kubes {kind: "kube", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#build] if u != _|_ {kind: "build", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.builds {kind: "build", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#image] if u != _|_ {kind: "image", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.images {kind: "image", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#artifact] if u != _|_ {kind: "artifact", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.artifacts {kind: "artifact", stem: u._stem, filename: u.#ref, service: u.#service, data: u},
	]
}
