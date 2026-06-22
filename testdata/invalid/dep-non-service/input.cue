package dep_non_service

import "github.com/lugoues/creidhne@v0"

// [Unit] dep fields take only #ServiceName; a .container ref is rejected.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: {Container: {Image: "docker.io/app:latest"}, Unit: After: ["app.container"]}
}
