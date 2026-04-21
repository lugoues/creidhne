package testing

import "github.com/lugoues/quadlets"

#Test: {
	subject:  quadlets.#Quadlet
	actual:   subject.output.quadlets
	expected: actual
}
