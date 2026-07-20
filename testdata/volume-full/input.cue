@extern(embed)

package volume_full

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "storage"
		units: {
			#volume: {
				Volume: {
					VolumeName:  "app-storage"
					ServiceName: "custom-storage"
					Driver:      "local"
					Type:        "tmpfs"
					Device: "/dev/sda1"
					Options: ["size=1G", "noexec"]
					Copy:  false
					User:  "appuser"
					Group: "appgroup"
					UID:   1000
					GID:   1000
					Image: "docker.io/volume-init:latest"
					Label: ["env=production", "team=platform"]
					GlobalArgs: ["--log-level=debug"]
					PodmanArgs: ["--opt=o=uid=1000"]
					ContainersConfModule: ["/etc/containers/storage.conf"]
				}
				Install: WantedBy: ["default.target"]
			}
		}
	}
	expected: {
		"storage.volume": _ @embed(file=expected/storage.volume,type=text)
	}
}
