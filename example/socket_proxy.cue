package example

// import "github.com/lugoues/quadlets@v0"
import "github.com/lugoues/quadlets"

socket_proxy: quadlets.#Quadlet & {
	name: "socket-proxy"

	units: {
		#container: {
			Unit: {
				Description: "Podman Socket Proxy"
				After: [externals.sockets.podman.#ref]
				Requires: [externals.sockets.podman.#ref]
			}

			Container: {
				User:          "0:0"
				Image:         "docker.io/wollomatic/socket-proxy:1"
				ContainerName: "socket-proxy"

				Environment: [
					"SP_LOGLEVEL=debug",
					"SP_LISTENIP=",
					"SP_SHUTDOWNGRACETIME=5",
					"SP_WATCHDOGINTERVAL=600",
					"SP_STOPONWATCHDOG=true",
					"SP_PROXYSOCKETENDPOINTFILEMODE=0666",
					"SP_PROXYSOCKETENDPOINT=/mnt/spx/socket-proxy.sock",
					#"SP_ALLOW_GET=/v1\\..{1,2}/(version|containers/.*|events.*)"#,
					"SP_ALLOW_HEAD=/_ping",
				]

				Volume: [
					"\(units.volumes.run.#ref):/mnt/spx",
					"/run/podman/podman.sock:/var/run/docker.sock:ro",
				]

				ReadOnly:        true
				DropCapability: ["ALL"]
				NoNewPrivileges: true
			}

			Service: {
				Restart:   "always"
				MemoryMax: "64M"
			}

			Install: WantedBy: ["multi-user.target", "default.target"]
		}

		volumes: run: {
			Volume: {
				Label: ["app=socket-proxy"]
			}
		}
	}
}
