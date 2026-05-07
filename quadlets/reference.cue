package quadlets

// #Reference computes #ref and #service for any quadlet unit.
// Embedded into each unit type schema (#Container, #Build, etc.)
// with the appropriate #unitType and #serviceSuffix.
//
// #ref is the quadlet filename reference (e.g., "traefik.container").
// #service is the systemd service name that quadlet generates
// (e.g., "traefik.service" for containers, "traefik-build.service" for builds).
#Reference: {
	#unitName:      *"" | string // empty for primary units, map key for plural
	#quadletName:   string
	#unitType:      string
	#serviceSuffix: string

	// Optional explicit name override. When set, used instead of #unitName
	// for the stem. Allows CUE-friendly identifiers (underscores) as map
	// keys while producing hyphenated filenames.
	//   volumes: gw_tmp: { name: "gw-tmp", ... }
	name?: string

	let _name = #quadletName
	let _key = {
		if name != _|_ {name}
		if name == _|_ {#unitName}
	}
	stem: {
		if _key == "" {_name}
		if _key != "" {"\(_name)-\(_key)"}
	}
	#ref:     "\(stem).\(#unitType)"
	#service: "\(stem)\(#serviceSuffix).service"
}
