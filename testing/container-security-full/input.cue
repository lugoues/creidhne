@extern(embed)

package container_security_full

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

_mode:     *"test" | "update" @tag(mode)
_expected: _ @embed(file=expected.quadlets,type=text)

test: testing.#Test & {
	subject: quadlets.#Quadlet & {
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
					Pod:                   "mypod.pod"
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
	if _mode == "test" {
		expected: _expected
	}
}
