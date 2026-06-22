package creidhne

// Strict dependency fields: the [Unit] section's unit-reference fields accept
// only #ServiceName (a systemd unit name), not an arbitrary string. This
// unifies with (tightens) the generated #UnitSection in systemd_sections.gen.cue
// without touching the generator. Reference a managed unit via its #service, or
// an external native unit (service/socket/target/...) via its #ref; a podman
// .container/.volume #self or a typo'd bare word is rejected here.
#UnitSection: {
	After?:                [...#ServiceName]
	Before?:               [...#ServiceName]
	Wants?:                [...#ServiceName]
	Requires?:             [...#ServiceName]
	Requisite?:            [...#ServiceName]
	BindsTo?:              [...#ServiceName]
	Upholds?:              [...#ServiceName]
	Conflicts?:            [...#ServiceName]
	PartOf?:               [...#ServiceName]
	OnFailure?:            [...#ServiceName]
	OnSuccess?:            [...#ServiceName]
	PropagatesReloadTo?:   [...#ServiceName]
	ReloadPropagatedFrom?: [...#ServiceName]
	PropagatesStopTo?:     [...#ServiceName]
	StopPropagatedFrom?:   [...#ServiceName]
	JoinsNamespaceOf?:     [...#ServiceName]
}
