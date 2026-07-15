package check_unfilled

import "github.com/lugoues/creidhne"

// A mixin with an unfilled required config must fail via its #checks entry
// instead of rendering inert.
#WebSpec: {
	#cfg: port!: int
	#checks: "web/cfg": {
		require: [#cfg.port]
		why: "fill #cfg when mixing #WebSpec"
	}
	...
}

app: creidhne.#Quadlet & #WebSpec & {
	name: "app"
	units: #container: Container: Image: "docker.io/app:latest"
}
