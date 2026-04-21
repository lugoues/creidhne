package quadlets

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

	// Primary units (singular, uses quadlet name directly)
	#container?: #Container & {#quadletName: _qn}
	#pod?:       #Pod & {#quadletName: _qn}
	#volume?:    #Volume & {#quadletName: _qn}
	#network?:   #Network & {#quadletName: _qn}
	#kube?:      #Kube & {#quadletName: _qn}
	#build?:     #Build & {#quadletName: _qn}
	#image?:     #Image & {#quadletName: _qn}
	#artifact?:  #Artifact & {#quadletName: _qn}

	// Additional units (plural, keyed, uses quadlet name + key)
	containers: [Key=string]: #Container & {#unitName: Key, #quadletName: _qn}
	pods: [Key=string]:       #Pod & {#unitName: Key, #quadletName: _qn}
	volumes: [Key=string]:    #Volume & {#unitName: Key, #quadletName: _qn}
	networks: [Key=string]:   #Network & {#unitName: Key, #quadletName: _qn}
	kubes: [Key=string]:      #Kube & {#unitName: Key, #quadletName: _qn}
	builds: [Key=string]:     #Build & {#unitName: Key, #quadletName: _qn}
	images: [Key=string]:     #Image & {#unitName: Key, #quadletName: _qn}
	artifacts: [Key=string]:  #Artifact & {#unitName: Key, #quadletName: _qn}
}

// #Quadlet is a self-contained, named deployment unit.
//
// Usage:
//
//   import "github.com/lugoues/quadlets@v0"
//
//   // Define an external dependency registry (optional helper pattern):
//   externals: quadlets.#ExternalUnits & {
//       targets: "network-online": _
//       sockets: podman: _
//   }
//
//   traefik: quadlets.#Quadlet & {
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
	output: Render & {#input: units}
}
