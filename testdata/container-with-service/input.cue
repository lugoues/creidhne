@extern(embed)

package container_with_service

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "app"
		units: {
			#container: {
				Container: {
					Image:         "docker.io/myapp:v2"
					ContainerName: "app"
					Environment: [
						"APP_ENV=production",
						"LOG_LEVEL=info",
					]
					Volume: [units.volumes.data.#self & {target: "/data"}]
					HealthCmd:       "curl -f http://localhost/health"
					HealthInterval:  "30s"
					HealthRetries:   3
					HealthTimeout:   "5s"
					Memory:          "512m"
					NoNewPrivileges: true
					DropCapability: ["ALL"]
				}
				Service: {
					Restart:   "always"
					MemoryMax: "1G"
					CPUQuota:  "50%"
				}
				Install: WantedBy: ["multi-user.target"]
			}

			volumes: data: {
				Volume: {}
			}
		}
	}
	expected: {
		"app-data.volume": _ @embed(file=expected/app-data.volume,type=text)
		"app.container": _ @embed(file=expected/app.container,type=text)
	}
}
