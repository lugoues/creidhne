package json_label

import "github.com/lugoues/creidhne"

// Golden coverage for #JSONLabel across every unit type that has Label=: a raw
// "key=value" string and a helper that JSON-encodes a structured payload both
// flatten into Label= lines.
#Spec: creidhne.#JSONLabel & {key: "app.spec"}

app: creidhne.#Quadlet & {
	name: "app"
	units: {
		#container: Container: {Image: "docker.io/app:latest", Label: ["plain=1", #Spec & {value: {kind: "container", note: "it's <b> & safe"}}]}
		#pod: Pod: Label: ["plain=1", #Spec & {value: {kind: "pod"}}]
		#volume: Volume: Label: ["plain=1", #Spec & {value: {kind: "volume"}}]
		#build: {ContainerFile: "FROM alpine\n", Build: Label: ["plain=1", #Spec & {value: {kind: "build"}}]}
	}
}
