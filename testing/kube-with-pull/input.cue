@extern(embed)

package kube_with_pull

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

_mode:     *"test" | "update" @tag(mode)
_expected: _ @embed(file=expected.quadlets,type=text)

test: testing.#Test & {
	subject: quadlets.#Quadlet & {
		name: "k8s-app"
		units: {
			#kube: {
				Kube: {
					Yaml: ["/opt/k8s/app.yaml"]
					ServiceName:         "k8s-app-custom"
					ConfigMap: ["/opt/k8s/config.yaml"]
					AutoUpdate: ["registry"]
					ExitCodePropagation: "any"
					KubeDownForce:       true
					LogDriver:           "journald"
					LogOpt: ["path=/var/log/kube.log"]
					Network: ["host"]
					PublishPort: ["8080:80"]
					UserNS:              "host"
					SetWorkingDirectory: "yaml"
				}
				Install: WantedBy: ["multi-user.target"]
			}
		}
	}
	if _mode == "test" {
		expected: _expected
	}
}
