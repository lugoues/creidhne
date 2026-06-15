package resource_name_ref

import "github.com/lugoues/creidhne@v0"

// Exercises the #<type>Name meta fields end-to-end: resolved podman resource
// names appear in another unit's rendered fields. Output is byte-asserted.

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
			// join cache's network namespace by its default resource name
			Network: ["container:\(cache.units.#container.#containerName)"]
			// link to db by its explicit resource name
			Environment: ["DB_HOST=\(db.units.#container.#containerName)"]
			// mount the volume by its default resource name
			Volume: ["\(data.units.#volume.#volumeName):/var/lib/app"]
		}
		Install: WantedBy: ["default.target"]
	}
}
