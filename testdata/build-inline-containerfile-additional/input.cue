@extern(embed)

package build_inline_containerfile_additional

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "myapp"
		units: {
			#container: {
				Container: {
					Image:         "localhost/myapp:latest"
					ContainerName: "myapp"
				}
				Install: WantedBy: ["default.target"]
			}

			builds: main: {
				ContainerFile: """
					FROM node:20
					WORKDIR /app
					COPY . .
					RUN npm ci && npm run build
					"""
				Build: ImageTag: ["localhost/myapp:latest"]
			}

			builds: sidecar: {
				name: "side-car"
				ContainerFile: """
					FROM golang:1.22
					COPY . .
					RUN go build -o /sidecar
					"""
				Build: {}
			}
		}
	}
	expected: {
		"myapp-main.build": _ @embed(file=expected/myapp-main.build,type=text)
		"myapp-side-car.build": _ @embed(file='expected/myapp-side-car.build',type=text)
		"myapp.container": _ @embed(file=expected/myapp.container,type=text)
		"images/myapp-main.Containerfile": _ @embed(file=expected/images/myapp-main.Containerfile,type=text)
		"images/myapp-side-car.Containerfile": _ @embed(file='expected/images/myapp-side-car.Containerfile',type=text)
	}
}
