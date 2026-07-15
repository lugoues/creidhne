package checks_case

import "github.com/lugoues/creidhne"

// Golden coverage for #checks: a satisfied require + assert changes nothing
// about the rendered output (the negative twin lives in invalid/check-unfilled).
#WebSpec: {
	#cfg: port!: int
	#checks: "web/cfg": {
		require: [#cfg.port]
		assert: true
		why:    "fill #cfg when mixing #WebSpec"
	}
	...
}

app: creidhne.#Quadlet & #WebSpec & {
	name: "app"
	#cfg: port: 8080
	units: #container: Container: {
		Image: "docker.io/app:latest"
		PublishPort: ["\(#cfg.port):80"]
	}
}
