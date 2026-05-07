@extern(embed)

package build_inline_containerfile_additional

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

_mode:     *"test" | "update" @tag(mode)
_expected: _ @embed(file=expected.quadlets,type=text)

test: testing.#Test & {
	subject: quadlets.#Quadlet & {
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
				ContainerFile: """
					FROM golang:1.22
					COPY . .
					RUN go build -o /sidecar
					"""
				Build: ImageTag: ["localhost/myapp-sidecar:latest"]
			}
		}
	}
	if _mode == "test" {
		expected: _expected
	}
}

// Verify the files map contains both Containerfiles.
_files: test.subject.output.files
_filesCheck: {
	mainContent: _files["images/myapp-main.Containerfile"] & """
		FROM node:20
		WORKDIR /app
		COPY . .
		RUN npm ci && npm run build
		"""
	sidecarContent: _files["images/myapp-sidecar.Containerfile"] & """
		FROM golang:1.22
		COPY . .
		RUN go build -o /sidecar
		"""
}
