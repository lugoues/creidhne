package tmpfs_bad_option

import "github.com/lugoues/creidhne@v0"

// huge=deny is sysfs-only and not a valid tmpfs MOUNT option, so #TmpfsOption
// rejects it in the typed form. (The raw-string form would accept anything; the
// point of the typed form is to catch options the kernel rejects at mount time.)
bad: creidhne.#Quadlet & {
	name: "bad"
	units: #container: Container: {
		Image: "docker.io/app:latest"
		Tmpfs: [{path: "/run", options: ["huge=deny"]}]
	}
}
