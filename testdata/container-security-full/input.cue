@extern(embed)

package container_security_full

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

externals: creidhne.#ExternalUnits & {pods: mypod: _}

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "secure"
		units: {
			#container: {
				Container: {
					Image:                 "docker.io/secure:latest"
					SecurityLabelDisable:  false
					SecurityLabelFileType: "container_file_t"
					SecurityLabelLevel:    "s0:c100,c200"
					SecurityLabelNested:   true
					SecurityLabelType:     "container_t"
					ReadOnly:              true
					ReadOnlyTmpfs:         true
					HttpProxy:             true
					Pod:                   externals.pods.mypod.#self
					StartWithPod:          true
					NoNewPrivileges:       true
					SeccompProfile:        "/etc/seccomp.json"
					AppArmor:              "container-default"
					Mask:                  "/proc/latency_stats"
					Unmask:                "/proc/acpi"
				}
				Install: WantedBy: ["default.target"]
			}
		}
	}
	expected: {
		"secure.container": _ @embed(file=expected/secure.container,type=text)
	}
}
