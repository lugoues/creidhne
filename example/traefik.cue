package example

// import "github.com/lugoues/quadlets@v0"
import "github.com/lugoues/quadlets"

traefik: quadlets.#Quadlet & {
	name: "traefik"

	units: {
		// Build the traefik image from a local Containerfile.
		#build: {
			Build: {
				BuildArg: ["TRAEFIK_VERSION=3.6.11"]
				ImageTag: ["localhost/traefik:quadlet"]
				SetWorkingDirectory: "/etc/containers/images/traefik"
				File:                "/etc/containers/images/traefik/Containerfile"
			}
		}

		// Main traefik reverse-proxy container.
		#container: {
			Unit: {
				Requires: [
					externals.targets["network-online"].#ref,
					units.volumes.acme.#service,
					units.volumes.plugins.#service,
					socket_proxy.units.volumes.run.#service,
				]
				After: [
					externals.targets["network-online"].#ref,
					units.volumes.acme.#service,
					units.volumes.plugins.#service,
					socket_proxy.units.volumes.run.#service,
				]
			}

			Container: {
				Image:         units.#build.#ref
				Pod:           "\(units.#pod.#ref)"
				ContainerName: "traefik"

				Secret: [
					"porkbun_api_key,type=env,target=PORKBUN_API_KEY",
					"porkbun_secret_api_key,type=env,target=PORKBUN_SECRET_API_KEY",
					"traefik-otel-auth,type=env,target=OTEL_AUTH",
				]

				Volume: [
					"/var/run/tailscale/tailscaled.sock:/var/run/tailscale/tailscaled.sock:ro",
					"\(units.volumes.acme.#ref):/etc/traefik/acme:U",
					"\(units.volumes.plugins.#ref):/traefik/plugins:U",
					"\(socket_proxy.units.volumes.run.#ref):/mnt/spx:ro",
				]

				ReadOnly:        true
				DropCapability: ["ALL"]
				NoNewPrivileges: true
				AutoUpdate:      "registry"
				AddCapability: ["NET_BIND_SERVICE"]

				HealthCmd:         "traefik healthcheck"
				HealthInterval:    "30s"
				HealthTimeout:     "3s"
				HealthRetries:     3
				HealthStartPeriod: "10s"

				Label: [
					"traefik.enable=true",

					// error pages middleware
					"traefik.http.middlewares.default-errors.errors.status=402-599",
					"traefik.http.middlewares.default-errors.errors.query=/{status}",
					"traefik.http.middlewares.default-errors.errors.service=default-errors",

					// dashboard
					"traefik.http.routers.dashboard.rule=Host(`traefik.fionn.lugoues.dev`)",
					"traefik.http.routers.dashboard.service=api@internal",
					"traefik.http.routers.dashboard.entrypoints=tailscale",

					// catch-all HTTPS
					"traefik.http.routers.default.rule=HostRegexp(`.+`)",
					"traefik.http.routers.default.priority=1",
					"traefik.http.routers.default.entrypoints=tailscale",
					"traefik.http.routers.default.service=default-errors",

					// socket proxy permissions
					"socket-proxy.allow.get=/v1\\..{1,2}/(version|containers/.*|events.*)",
					"socket-proxy.allow.head=/_ping:",
				]
			}

			Service: {
				Restart:   "always"
				MemoryMax: "2G"
			}

			Install: WantedBy: ["multi-user.target", "default.target"]
		}

		// Error page sidecar running in the same pod.
		containers: errors: {
			Container: {
				Image:         "docker.io/11notes/traefik:errors"
				Pod:           "\(units.#pod.#ref)"
				ContainerName: "traefik-errors"

				ReadOnly:        true
				DropCapability: ["ALL"]
				NoNewPrivileges: true

				Label: [
					"traefik.enable=true",
					"traefik.http.services.default-errors.loadbalancer.server.port=3000",
				]
			}

			Service: {
				Restart:      "always"
				Type:         "notify"
				NotifyAccess: "all"
			}

			Install: WantedBy: ["multi-user.target", "default.target"]
		}

		// Pod grouping traefik + error sidecar.
		#pod: {
			Unit: {
				Requires: [
					units.networks.internal.#service,
					externals.networks["internet-egress"].#service,
					externals.targets["network-online"].#ref,
					externals.services.tailscaled.#ref,
				]
				After: [
					units.networks.internal.#service,
					externals.networks["internet-egress"].#service,
					externals.targets["network-online"].#ref,
					externals.services.tailscaled.#ref,
				]
			}

			Pod: {
				PodName: "traefik"
				UserNS:  "auto"
				Network: [
					units.networks.internal.#ref,
					externals.networks["internet-egress"].#ref,
				]
				PublishPort: ["0.0.0.0:443:443"]
			}
		}

		// Internal network for traefik <-> backend communication.
		networks: internal: {
			Network: {
				Internal: true
				Options: ["isolate=true"]
			}
		}

		// Named volumes for persistent data.
		volumes: {
			acme: {}
			plugins: {}
		}
	}
}
