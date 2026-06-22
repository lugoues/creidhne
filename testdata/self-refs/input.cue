package self_refs

import "github.com/lugoues/creidhne"

// End-to-end golden coverage for the #self render paths the other fixtures miss:
// a volume #self with options (3-part source:target:options), Image= via a
// build #self (.build), a #MountRef (type=volume,source=...,destination=...),
// and Build Network=/Volume= via #self.
svc: creidhne.#Quadlet & {
	name: "svc"
	units: {
		networks: net: {Network: {}}
		volumes: data: {Volume: {}}

		#build: {
			ContainerFile: "FROM scratch\n"
			Build: {
				ImageTag: ["localhost/svc:latest"]
				Network: [units.networks.net.#self]
				Volume: [units.volumes.data.#self & {target: "/build"}]
			}
		}

		#container: {
			Container: {
				Image:         units.#build.#self
				ContainerName: "svc"
				Volume: [units.volumes.data.#self & {target: "/data", options: "ro,U"}]
				Mount: [creidhne.#MountRef & {ref: units.volumes.data.#self, destination: "/mnt", options: ["ro"]}]
			}
			Install: WantedBy: ["default.target"]
		}
	}
}
