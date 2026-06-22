@extern(embed)

package resource_name_ref

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

// Exercises the #<type>Name meta fields end-to-end: resolved podman resource
// names appear in another unit's rendered fields. Output is byte-asserted.
test: testing.#Test & {
	subjects: {
		// Explicit ContainerName -> "postgres".
		db: creidhne.#Quadlet & {
			name: "db"
			units: #container: {
				Container: {Image: "docker.io/postgres:16", ContainerName: "postgres"}
				Install: WantedBy: ["default.target"]
			}
		}

		// No ContainerName -> default "systemd-cache".
		cache: creidhne.#Quadlet & {
			name: "cache"
			units: #container: {
				Container: {Image: "docker.io/redis:7"}
				Install: WantedBy: ["default.target"]
			}
		}

		// No VolumeName -> default "systemd-data".
		data: creidhne.#Quadlet & {
			name: "data"
			units: #volume: {Volume: {}}
		}

		// app references all three by their resolved names.
		app: creidhne.#Quadlet & {
			name: "app"
			units: #container: {
				Container: {
					Image: "docker.io/app:latest"
					// join cache's network namespace via its #self handle (.container)
					Network: [test.subjects.cache.units.#container.#self]
					// link to db by its explicit resource name
					Environment: ["DB_HOST=\(test.subjects.db.units.#container.#containerName)"]
					// mount the managed volume via its #self handle
					Volume: [test.subjects.data.units.#volume.#self & {target: "/var/lib/app"}]
				}
				Install: WantedBy: ["default.target"]
			}
		}
	}
	expected: {
		"db.container": _ @embed(file=expected/db.container,type=text)
		"cache.container": _ @embed(file=expected/cache.container,type=text)
		"data.volume": _ @embed(file=expected/data.volume,type=text)
		"app.container": _ @embed(file=expected/app.container,type=text)
	}
}
