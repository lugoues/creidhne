@extern(embed)

package cross_quadlet_ref

import "github.com/lugoues/quadlets@v0"

// Two quadlets where one references the other's volume and service.
// Tests that #ref and #service resolve correctly across quadlet boundaries.

proxy: quadlets.#Quadlet & {
	name: "proxy"
	units: {
		#container: {
			Container: {
				Image:         "docker.io/haproxy:latest"
				ContainerName: "proxy"
				Network: ["backend.network"]
			}
			Unit: {
				After:    [app.units.#container.#service]
				Requires: [app.units.#container.#service]
			}
			Install: WantedBy: ["multi-user.target"]
		}

		networks: backend: {
			Network: Internal: true
		}
	}
}

app: quadlets.#Quadlet & {
	name: "app"
	units: {
		#container: {
			Container: {
				Image:         "docker.io/myapp:latest"
				ContainerName: "app"
				Volume: ["\(app.units.#volume.#ref):/data"]
				Network: ["proxy-backend.network"]
			}
			Install: WantedBy: ["multi-user.target"]
		}

		#volume: {
			Volume: {}
		}
	}
}

// Verify proxy output files
_proxyExpected: {
	"proxy.container":       _ @embed(file=expected/proxy.container,type=text)
	"proxy-backend.network": _ @embed(file=expected/proxy-backend.network,type=text)
}
_proxyActual: proxy.output.files
_proxyCheck: {
	for k, v in _proxyExpected {
		(k): _proxyActual[k] & v
	}
}

// Verify app output files
_appExpected: {
	"app.container": _ @embed(file=expected/app.container,type=text)
	"app.volume":    _ @embed(file=expected/app.volume,type=text)
}
_appActual: app.output.files
_appCheck: {
	for k, v in _appExpected {
		(k): _appActual[k] & v
	}
}
