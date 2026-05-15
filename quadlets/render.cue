@experiment(try)

package quadlets

import (
	"text/template"

	"github.com/lugoues/quadlets/templates@v0"
)

// Render dispatches template execution over a #Units value.
// Produces per-type maps of rendered unit files and a combined files output.
//
// Primary units (#container, #pod, etc.) are merged into the plural maps
// (containers, pods, etc.) keyed by their stem, so output.containers
// contains all containers regardless of how they were declared.
Render: {
	#input: #Units

	// --- Rendered unit maps (primary + additional merged) ---

	containers: {
		try {
			("\(#input.#container?.stem)"): template.Execute(templates.Container, #input.#container?)
		}
		for name, def in #input.containers {
			("\(def.stem)"): template.Execute(templates.Container, def)
		}
	}
	pods: {
		try {
			("\(#input.#pod?.stem)"): template.Execute(templates.Pod, #input.#pod?)
		}
		for name, def in #input.pods {
			("\(def.stem)"): template.Execute(templates.Pod, def)
		}
	}
	volumes: {
		try {
			("\(#input.#volume?.stem)"): template.Execute(templates.Volume, #input.#volume?)
		}
		for name, def in #input.volumes {
			("\(def.stem)"): template.Execute(templates.Volume, def)
		}
	}
	networks: {
		try {
			("\(#input.#network?.stem)"): template.Execute(templates.Network, #input.#network?)
		}
		for name, def in #input.networks {
			("\(def.stem)"): template.Execute(templates.Network, def)
		}
	}
	kubes: {
		try {
			("\(#input.#kube?.stem)"): template.Execute(templates.Kube, #input.#kube?)
		}
		for name, def in #input.kubes {
			("\(def.stem)"): template.Execute(templates.Kube, def)
		}
	}
	builds: {
		try {
			let _b = #input.#build?
			("\(_b.stem)"): template.Execute(templates.Build, {
				_b
				if _b.ContainerFile != _|_ {
					containerfilePath: "images/\(_b.stem).Containerfile"
					if _b.Context != _|_ {
						contextPath: "images/\(_b.stem).context"
					}
				}
			})
		}
		for name, def in #input.builds {
			("\(def.stem)"): template.Execute(templates.Build, {
				def
				if def.ContainerFile != _|_ {
					containerfilePath: "images/\(def.stem).Containerfile"
					if def.Context != _|_ {
						contextPath: "images/\(def.stem).context"
					}
				}
			})
		}
	}
	images: {
		try {
			("\(#input.#image?.stem)"): template.Execute(templates.Image, #input.#image?)
		}
		for name, def in #input.images {
			("\(def.stem)"): template.Execute(templates.Image, def)
		}
	}
	artifacts: {
		try {
			("\(#input.#artifact?.stem)"): template.Execute(templates.Artifact, #input.#artifact?)
		}
		for name, def in #input.artifacts {
			("\(def.stem)"): template.Execute(templates.Artifact, def)
		}
	}

	// --- Individual file map (filename -> content) ---

	files: {
		for stem, content in containers { "\(stem).container": content }
		for stem, content in pods { "\(stem).pod": content }
		for stem, content in volumes { "\(stem).volume": content }
		for stem, content in networks { "\(stem).network": content }
		for stem, content in kubes { "\(stem).kube": content }
		for stem, content in builds { "\(stem).build": content }
		for stem, content in images { "\(stem).image": content }
		for stem, content in artifacts { "\(stem).artifact": content }

		// Inline Containerfiles and context files from builds.
		// Context values are normalized to {content, mode} structs.
		try {
			if #input.#build?.ContainerFile != _|_ {
				"images/\(#input.#build?.stem).Containerfile": #input.#build?.ContainerFile
			}
			if #input.#build?.Context != _|_ {
				for _path, _val in #input.#build?.Context {
					let _norm = {
						if (_val & string) != _|_ {
							content: _val
							mode:    "0644"
						}
						if (_val & string) == _|_ {
							_val
						}
					}
					"images/\(#input.#build?.stem).context/\(_path)": _norm
				}
			}
		}
		for _name, def in #input.builds {
			if def.ContainerFile != _|_ {
				"images/\(def.stem).Containerfile": def.ContainerFile
			}
			if def.Context != _|_ {
				for _path, _val in def.Context {
					let _norm = {
						if (_val & string) != _|_ {
							content: _val
							mode:    "0644"
						}
						if (_val & string) == _|_ {
							_val
						}
					}
					"images/\(def.stem).context/\(_path)": _norm
				}
			}
		}
	}

}
