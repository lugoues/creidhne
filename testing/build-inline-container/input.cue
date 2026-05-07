@extern(embed)

package build_inline_containerfile

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
	if _mode == "test" {
		expected: _expected
	}
}

// Verify the files map contains the Containerfile content.
_files: test.subject.output.files
_filesCheck: {
	hasContainerfile:      _files["images/myapp.Containerfile"] != _|_
	hasBuild:              _files["myapp.build"] != _|_
	containerfileContent:  _files["images/myapp.Containerfile"]
	containerfileExpected: containerfileContent & """
		FROM node:20
		WORKDIR /app
		COPY . .
		RUN npm ci && npm run build
		"""
}
