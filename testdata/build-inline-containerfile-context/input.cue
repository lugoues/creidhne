@extern(embed)

package build_inline_containerfile_context

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
	expected: {
		"myapp.build": _ @embed(file=expected/myapp.build,type=text)
		"images/myapp.Containerfile": _ @embed(file=expected/images/myapp.Containerfile,type=text)
		"images/myapp.context/etc/app.conf": _ @embed(file=expected/images/myapp.context/etc/app.conf,type=text)
		"images/myapp.context/scripts/entrypoint.sh": _ @embed(file=expected/images/myapp.context/scripts/entrypoint.sh,type=text)
	}
}
