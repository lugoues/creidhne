package testing

import "github.com/lugoues/quadlets"

#Test: {
	subject: quadlets.#Quadlet

	// Normalize output.files: extract .content from {content, mode} structs,
	// pass strings through unchanged. This lets expected be a plain string map.
	_actual: {
		for _k, _v in subject.output.files {
			if (_v & string) != _|_ {
				(_k): _v
			}
			if (_v & string) == _|_ {
				(_k): _v.content
			}
		}
	}

	expected: _actual
}
