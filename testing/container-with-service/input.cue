@extern(embed)

package container_with_service

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: quadlets.#Quadlet & {
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
					Volume: ["app-data.volume:/data"]
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
