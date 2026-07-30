package creidhne

// #AssetRef marks a build-context entry whose content comes from files in the
// project directory, resolved by crei at load time. `asset` is a
// project-relative glob (doublestar syntax: `*`, `?`, `[...]`, and `**` for
// recursive matching). The matched files are expanded into ordinary context
// entries under the referencing Context key, preserving their path relative to
// the glob's static prefix, so the build stays fully deterministic: the file
// bytes are hashed into the build's content hash and emitted into
// images/<stem>.context/ like inline content. A glob that matches nothing is a
// load error, never a silently empty context.
#AssetRef: {
	asset: string & !=""
}

// #AssetRegistry is a named set of project asset globs — files too large or
// too numerous to inline in CUE (dashboards, provisioning trees, config
// bundles), declared once and referenced from build contexts by handle.
//
// Unlike the image and secret registries, registries/assets.cue is
// hand-authored: crei reads it but never rewrites it (there is nothing to
// write back), so comments there are safe.
//
//   // registries/assets.cue
//   assets: creidhne.#AssetRegistry & {
//       grafana_dashboards: source: "assets/grafana/dashboards/**/*.json"
//   }
//
// Reference an entry from a build's Context; the key is the destination
// directory inside the context ("." for the context root):
//
//   #build: Context: dashboards: reg.assets.grafana_dashboards.#ref
#AssetRegistry: [Key=string]: #AssetEntry

#AssetEntry: {
	// source is the project-relative glob the entry resolves to.
	source: string & !=""

	// #ref is the handle a build context consumes; computed, like the image
	// registry's #ref.
	#ref: #AssetRef & {asset: source}
}
