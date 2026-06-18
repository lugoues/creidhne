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

	// Primary units (singular): stem is the quadlet name.
	#container?: #Container & {#stem: _qn}
	#pod?:       #Pod & {#stem: _qn}
	#volume?:    #Volume & {#stem: _qn}
	#network?:   #Network & {#stem: _qn}
	#kube?:      #Kube & {#stem: _qn}
	#build?:     #Build & {#stem: _qn}
	#image?:     #Image & {#stem: _qn}
	#artifact?:  #Artifact & {#stem: _qn}

	// Additional units (plural, keyed): stem is "<quadlet>-<key>".
	containers: [Key=string]: #Container & {#stem: "\(_qn)-\(Key)"}
	pods: [Key=string]:       #Pod & {#stem: "\(_qn)-\(Key)"}
	volumes: [Key=string]:    #Volume & {#stem: "\(_qn)-\(Key)"}
	networks: [Key=string]:   #Network & {#stem: "\(_qn)-\(Key)"}
	kubes: [Key=string]:      #Kube & {#stem: "\(_qn)-\(Key)"}
	builds: [Key=string]:     #Build & {#stem: "\(_qn)-\(Key)"}
	images: [Key=string]:     #Image & {#stem: "\(_qn)-\(Key)"}
	artifacts: [Key=string]:  #Artifact & {#stem: "\(_qn)-\(Key)"}
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
	name: string
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
		for u in [units.#container] if u != _|_ {kind: "container", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.containers {kind: "container", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#pod] if u != _|_ {kind: "pod", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.pods {kind: "pod", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#volume] if u != _|_ {kind: "volume", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.volumes {kind: "volume", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#network] if u != _|_ {kind: "network", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.networks {kind: "network", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#kube] if u != _|_ {kind: "kube", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.kubes {kind: "kube", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#build] if u != _|_ {kind: "build", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.builds {kind: "build", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#image] if u != _|_ {kind: "image", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.images {kind: "image", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for u in [units.#artifact] if u != _|_ {kind: "artifact", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
		for _, u in units.artifacts {kind: "artifact", stem: u.#stem, filename: u.#ref, service: u.#service, data: u},
	]
}
