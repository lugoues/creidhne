@extern(embed)

package container_secrets

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
					Image: "docker.io/myapp:latest"
					Secret: [
						// Raw string
						"raw-secret,type=env,target=RAW_VAR",
						// Struct: env type
						{
							name:   "my-api-key"
							type:   "env"
							target: "API_KEY"
						},
						// Struct: mount type with permissions
						{
							name:   "tls-cert"
							type:   "mount"
							target: "/etc/ssl/cert.pem"
							uid:    1000
							gid:    1000
							mode:   "0400"
						},
						// Struct: minimal (name only)
						{
							name: "simple-secret"
						},
					]
				}
				Install: WantedBy: ["default.target"]
			}
		}
	}
	expected: {
		"app.container": _ @embed(file=expected/app.container,type=text)
	}
}
