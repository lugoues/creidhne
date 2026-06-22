package image_raw_build_ref

import "github.com/lugoues/creidhne@v0"

// A .build/.image ref written as a raw string is rejected; use #self.
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {Image: "app.build"}
}
