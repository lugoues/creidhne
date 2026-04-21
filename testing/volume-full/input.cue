@extern(embed)

package volume_full

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

_mode:     *"test" | "update" @tag(mode)
_expected: _ @embed(file=expected.quadlets,type=text)

test: testing.#Test & {
	subject: quadlets.#Quadlet & {
		name: "storage"
		units: {
			#volume: {
				Volume: {
					VolumeName:  "app-storage"
					ServiceName: "custom-storage"
					Driver:      "local"
					Type:        "tmpfs"
					Device: ["/dev/sda1", "/dev/sdb1"]
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
	if _mode == "test" {
		expected: _expected
	}
}
