@extern(embed)

package container_telemetry_labels

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

// Telemetry log labels: the primary container carries a pinned image (IMAGE +
// IMAGE_DIGEST split on '@'); a plural container shows QUADLET_UNIT_NAME differ
// from QUADLET; a driver override suppresses the labels; a Rootfs container
// omits IMAGE.
test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "web"
		units: {
			#container: Container: {
				Image:         "ghcr.io/acme/web:2.3.0@sha256:6339abc0000000000000000000000000000000000000000000000000000000ff"
				ContainerName: "web"
			}
			containers: worker: Container: Image: "docker.io/library/redis:7.2"
			containers: nolog: Container: {
				Image:     "docker.io/x:1"
				LogDriver: "k8s-file"
			}
			containers: roots: Container: Rootfs: "/var/lib/x"
		}
	}
	expected: {
		"web.container":        _ @embed(file=expected/web.container,type=text)
		"web-worker.container": _ @embed(file=expected/web-worker.container,type=text)
		"web-nolog.container":  _ @embed(file=expected/web-nolog.container,type=text)
		"web-roots.container":  _ @embed(file=expected/web-roots.container,type=text)
	}
}
