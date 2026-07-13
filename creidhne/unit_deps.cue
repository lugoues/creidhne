package creidhne

// Strict dependency fields: the [Unit] section's unit-reference fields accept
// only #ServiceName (a systemd unit name), not an arbitrary string. This
// unifies with (tightens) the generated #UnitSection in systemd_sections.gen.cue
// without touching the generator. Reference a managed unit via its #service, or
// an external native unit (service/socket/target/...) via its #ref; a podman
// .container/.volume #self or a typo'd bare word is rejected here.
#UnitSection: {
	After?: [...(#ServiceName | [...#ServiceName])]
	Before?: [...(#ServiceName | [...#ServiceName])]
	Wants?: [...(#ServiceName | [...#ServiceName])]
	Requires?: [...(#ServiceName | [...#ServiceName])]
	Requisite?: [...(#ServiceName | [...#ServiceName])]
	BindsTo?: [...(#ServiceName | [...#ServiceName])]
	Upholds?: [...(#ServiceName | [...#ServiceName])]
	Conflicts?: [...(#ServiceName | [...#ServiceName])]
	PartOf?: [...(#ServiceName | [...#ServiceName])]
	OnFailure?: [...(#ServiceName | [...#ServiceName])]
	OnSuccess?: [...(#ServiceName | [...#ServiceName])]
	PropagatesReloadTo?: [...(#ServiceName | [...#ServiceName])]
	ReloadPropagatedFrom?: [...(#ServiceName | [...#ServiceName])]
	PropagatesStopTo?: [...(#ServiceName | [...#ServiceName])]
	StopPropagatedFrom?: [...(#ServiceName | [...#ServiceName])]
	JoinsNamespaceOf?: [...(#ServiceName | [...#ServiceName])]
}
