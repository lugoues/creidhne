package tmpfs_options

import "github.com/lugoues/creidhne"

// Golden coverage for typed Tmpfs= options (#TmpfsSpec) rendering to
// "path:opt,opt", alongside the raw-string escape-hatch form. Mixed in one list
// to exercise the tmpfsStrings flatten over a string | #TmpfsSpec disjunction.
app: creidhne.#Quadlet & {
	name: "app"
	units: #container: Container: {
		Image: "docker.io/app:latest"
		Tmpfs: [
			"/run:rw,size=64m",                                                                          // raw string (escape hatch)
			{path: "/tmp"},                                                                              // typed, no options -> bare path
			{path: "/var/cache", options: ["rw", "nosuid", "nodev", "noexec", "size=128m", "mode=1777"]}, // typed flags + key=values
		]
	}
}
