package userns_bad_option

import "github.com/lugoues/creidhne"

// A typo'd option key inside a typed #UserNSSpec must be rejected (the raw
// string form would have let "uidd" through).
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: Container: {
		Image: "docker.io/app"
		UserNS: {mode: "keep-id", uidd: 1000}
	}
}
