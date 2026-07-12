package typed_specs

import "github.com/lugoues/creidhne"

// Golden coverage for typed #UlimitSpec and #UserNSSpec alongside their raw
// string escape hatches, plus LogOpt's key=value constraint. Ulimit and
// LogOpt each nest one element to prove the loosened list types flatten.
app: creidhne.#Quadlet & {
	name: "app"
	units: {
		#container: Container: {
			Image: "docker.io/app:latest"
			Ulimit: [
				"host",                                   // raw escape hatch
				{name: "nofile", soft: 1024, hard: 2048}, // typed
				[{name: "nproc", soft: -1}, "stack=8192"], // nested block, mixed forms
			]
			UserNS: {mode: "keep-id", uid: 1000, gid: 1000}
			LogDriver: "journald"
			LogOpt: ["tag=app", ["max-size=10mb", "max-file=3"]]
		}
		#pod: Pod: UserNS: {mode: "auto", size: 500, uidmapping: ["0:100:10"]}
	}
}
