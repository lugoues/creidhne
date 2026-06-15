@extern(embed)

package container_device_idmap

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

// Exercises the tightened #DeviceMapping and #IDMap validators across their
// documented forms: a bare device, an optional ("-") device, a device with an
// explicit container path + permissions, and UID/GID maps using the basic,
// "+" (extend) and "@" (host-id) notations.
test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "devmap"
		units: {
			#container: {
				Container: {
					Image: "docker.io/app:latest"
					AddDevice: [
						"/dev/fuse",
						"-/dev/kvm",
						"/dev/net/tun:/dev/net/tun:rwm",
					]
					UIDMap: ["0:100000:65536", "+1:@1000:1"]
					GIDMap: ["0:100000:65536"]
				}
				Install: WantedBy: ["multi-user.target"]
			}
		}
	}
	expected: {
		"devmap.container": _ @embed(file=expected/devmap.container,type=text)
	}
}
