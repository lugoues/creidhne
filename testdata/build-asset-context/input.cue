@extern(embed)

package build_asset_context

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

// Asset-ref build context: a recursive glob over project files expands into
// inline context entries (structure preserved under the Context key), the
// bytes feed the build content hash, and the consuming container carries the
// same hash. The mode of an executable asset survives (run.sh is 0755 in git).
test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "grafana"
		units: {
			#build: {
				ContainerFile: """
					FROM docker.io/grafana/grafana
					COPY dashboards /etc/grafana/dashboards
					"""
				Context: {
					dashboards: creidhne.#AssetRef & {asset: "assets/dashboards/**/*.json"}
					"provision.sh": "#!/bin/sh\necho provision\n"
				}
			}
			#container: Container: Image: units.#build.#self
		}
	}
	expected: {
		"grafana.build":     _ @embed(file=expected/grafana.build,type=text)
		"grafana.container": _ @embed(file=expected/grafana.container,type=text)
		"images/grafana.Containerfile": _ @embed(file=expected/images/grafana.Containerfile,type=text)
		"images/grafana.context/provision.sh": _ @embed(file=expected/images/grafana.context/provision.sh,type=text)
		"images/grafana.context/dashboards/overview.json": _ @embed(file=expected/images/grafana.context/dashboards/overview.json,type=text)
		"images/grafana.context/dashboards/host/node.json": _ @embed(file=expected/images/grafana.context/dashboards/host/node.json,type=text)
	}
}
