package testing

import "github.com/lugoues/creidhne"

// #Test wraps the quadlet subject(s) for a golden fixture. A fixture sets
// exactly one of:
//   subject  — a single quadlet
//   subjects — several keyed quadlets, for cross-quadlet references
//
// Output assertion lives in the Go golden test harness, which renders each
// subject's manifest through the text/templates and byte-compares against the
// files under the case's expected/ directory. This definition therefore renders
// nothing in CUE; it constrains the subject(s) to valid #Quadlets (so `cue vet`
// validates fixture inputs against the schema) and accepts the embedded
// `expected` string map the fixtures carry.
#Test: {
	subject?: creidhne.#Quadlet
	subjects?: [string]: creidhne.#Quadlet
	expected?: [string]: string

	matchN(1, [{subject!: _}, {subjects!: _}]) // exactly one of subject/subjects
}
