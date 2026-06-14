@extern(embed)

package container_health_full

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "healthcheck"
		units: {
			#container: {
				Container: {
					Image:                 "docker.io/healthapp:latest"
					HealthCmd:             "curl -f http://localhost:8080/health"
					HealthInterval:        "30s"
					HealthRetries:         3
					HealthStartPeriod:     "10s"
					HealthTimeout:         "5s"
					HealthOnFailure:       "restart"
					HealthLogDestination:  "/var/log/health"
					HealthMaxLogCount:     10
					HealthMaxLogSize:      "1m"
					HealthStartupCmd:      "curl -f http://localhost:8080/ready"
					HealthStartupInterval: "5s"
					HealthStartupRetries:  5
					HealthStartupSuccess:  2
					HealthStartupTimeout:  "10s"
				}
				Install: WantedBy: ["default.target"]
			}
		}
	}
	expected: {
		"healthcheck.container": _ @embed(file=expected/healthcheck.container,type=text)
	}
}
