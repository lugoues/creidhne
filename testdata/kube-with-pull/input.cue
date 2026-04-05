@extern(embed)

package kube_with_pull

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
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
	expected: {
		"k8s-app.kube": _ @embed(file=expected/k8s-app.kube,type=text)
	}
}
