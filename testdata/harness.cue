package testing

import "github.com/lugoues/creidhne"

// #Test wraps a subject quadlet for the golden fixtures.
//
// Output assertion now lives in the Go golden test harness, which renders the
// subject's manifest through the text/templates and byte-compares against the
// files under each case's expected/ directory. This definition therefore no
// longer renders or asserts anything in CUE; it just constrains `subject` to a
// valid #Quadlet (so `cue vet` still validates fixture inputs against the
// schema) and accepts the embedded `expected` string map the fixtures carry.
#Test: {
	subject: creidhne.#Quadlet
	expected?: [string]: string
}
