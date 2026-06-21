package both_subjects

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

// A fixture sets exactly one of subject/subjects; setting both violates the
// harness's matchN(1, ...) constraint.
test: testing.#Test & {
	subject: creidhne.#Quadlet & {name: "a", units: #container: Container: Image: "img"}
	subjects: b: creidhne.#Quadlet & {name: "b", units: #container: Container: Image: "img"}
}
