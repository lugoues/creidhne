@extern(embed)

package build_inline_containerfile_context

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
					COPY etc/app.conf /etc/app.conf
					COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh
					"""
				Context: {
					"etc/app.conf": """
						[app]
						debug = false
						"""
					"scripts/entrypoint.sh": {
						content: """
							#!/bin/bash
							exec node /app/server.js
							"""
						mode: "0755"
					}
				}
				Build: ImageTag: ["localhost/myapp:latest"]
			}
		}
	}
	if _mode == "test" {
		expected: _expected
	}
}

// Verify the files map contains context files.
_files: test.subject.output.files
_filesCheck: {
	hasContainerfile: _files["images/myapp.Containerfile"] != _|_
	hasBuild:         _files["myapp.build"] != _|_
	hasAppConf:       _files["images/myapp.context/etc/app.conf"] != _|_
	hasEntrypoint:    _files["images/myapp.context/scripts/entrypoint.sh"] != _|_

	appConfContent: _files["images/myapp.context/etc/app.conf"].content & """
		[app]
		debug = false
		"""
	appConfMode: _files["images/myapp.context/etc/app.conf"].mode & "0644"
	entrypointContent: _files["images/myapp.context/scripts/entrypoint.sh"].content & """
		#!/bin/bash
		exec node /app/server.js
		"""
	entrypointMode: _files["images/myapp.context/scripts/entrypoint.sh"].mode & "0755"
}
