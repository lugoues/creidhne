@extern(embed)

package build_inline_containerfile

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "myapp"
		units: {
			#build: {
				ContainerFile: """
					FROM node:20
					WORKDIR /app
					COPY . .
					RUN npm ci && npm run build
					"""
				Build: ImageTag: ["localhost/myapp:latest"]
			}
		}
	}
	expected: {
		"myapp.build": _ @embed(file=expected/myapp.build,type=text)
		"images/myapp.Containerfile": _ @embed(file=expected/images/myapp.Containerfile,type=text)
	}
}
