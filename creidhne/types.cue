package creidhne

import (
	"encoding/json"
	"list"
	"strings"
)

// Common types used across all Quadlet unit definitions.

// #UnitName constrains a quadlet/unit name. It feeds the on-disk filename
// (stem), so it must be a single safe path segment: no separators, no "..", no
// leading dot/dash. This is the validation half of the path-traversal defense;
// render/reconcile enforce filepath.IsLocal as a belt-and-suspenders layer.
#UnitName: string & =~"^[a-zA-Z0-9][a-zA-Z0-9_.-]*$"

// Port mapping: [ip:]hostPort[-range][:containerPort[-range]][/protocol]. The
// host IP may be IPv4 or a bracketed IPv6 address ([::1]:80:90), matching
// podman/quadlet (see quadlet's ports_ipv6.container test).
#PortMapping: =~"^((\\[[0-9a-fA-F:]+\\]|[0-9.]*):)?[0-9]+(-[0-9]+)?(:[0-9]+(-[0-9]+)?)?(/(tcp|udp|sctp))?$" | =~"^[0-9]+(-[0-9]+)?$"

// Key=Value pair for labels, annotations, environment variables
#KeyValue: =~"^[^=]+=.*$"

// #Rendered carries a pre-computed label in its #rendered field: one
// "key=value" string, or a list of them for a helper that expands to several
// labels (the label flatteners splice a list in place). #rendered is a
// definition field, visible across packages, so any package can build label
// helpers by unifying #Rendered and setting #rendered (a hidden _rendered
// would be package-scoped and only producible here). Definition fields never
// survive export, so #rendered stays out of the manifest; the xStrings
// comprehensions extract it before Go renders. Open (...) so a helper
// carrying extra fields (like #JSONLabel's key/value) still satisfies it.
#Rendered: {
	#rendered: #KeyValue | [...#KeyValue]

	// #renderedList normalizes #rendered for extraction: a scalar wraps into a
	// one-element list (the default arm), a list passes through (the scalar arm
	// errors out of the disjunction and drops). When #rendered was never made
	// concrete, neither arm errors, the default survives holding the unresolved
	// constraint, and the render fails loud instead of dropping the label:
	// comprehension guards cannot make that distinction (an unset #rendered is
	// incomplete, not erroneous, and unifies with [...] as an empty splice).
	#renderedList: *[#rendered & string] | (#rendered & [...#KeyValue])
	...
}

// #LabelValue accepts a raw "key=value" string or a #Rendered helper.
#LabelValue: #KeyValue | #Rendered

// _#renderLabel resolves one Label element to a flat list of label strings: a
// raw string passes through; a #Rendered helper contributes its normalized
// #renderedList, one label or several, spliced in place.
_#renderLabel: {
	#e: _
	out: [
		if (#e & string) != _|_ {#e},
		if (#e & string) == _|_ for x in (#e & #Rendered).#renderedList {x},
	]
}

// #JSONLabel renders "key=<json(value)>": a structured payload encoded as the
// label's value, so callers stop hand-rolling json.Marshal in an interpolation.
// value is an open struct (marshaling it, not the whole helper, keeps #rendered
// out of the payload — no self-reference). An empty value marshals to "{}", so
// the schema type-checks without a concrete payload.
#JSONLabel: #Rendered & {
	key: string
	value: {...}
	// Single-quote the whole key=value: quadlet word-splits Label= values
	// (systemd syntax), so the raw JSON would break the parse. HTMLEscape turns
	// <, >, & into \uXXXX; a literal ' would still terminate the single-quoting,
	// so replace it with its \uXXXX escape as well. Both keep the value valid
	// JSON that the consumer decodes back.
	#rendered: "'\(key)=\(strings.Replace(json.HTMLEscape(json.Marshal(value)), "'", "\\u0027", -1))'"
}

// CIDR notation
#CIDR: =~"^[0-9a-fA-F:.]+/[0-9]+$"

// IP range: CIDR notation or startIP-endIP
#IPRange: #CIDR | =~"^[0-9a-fA-F:.]+-[0-9a-fA-F:.]+$"

// Volume mount for Volume= ([Container]/[Pod]/[Build]).
// Form: [[SOURCE-VOLUME|HOST-DIR:]CONTAINER-DIR[:OPTIONS]] (podman-systemd.unit.5
// Volume=, equivalent to podman-run --volume). The container dir is absolute;
// the source and the option list never contain a colon, so a value is one to
// three colon-separated segments:
//   /dest                anonymous volume
//   source:/dest         named volume or host bind
//   source:/dest:opts    with mount options (ro, z, U, ...)
#VolumeMount: =~"^(/[^:]+|[^:]+:/[^:]+(:[^:]+)?)$"

// #HostMount is the strict raw Volume= form: a host bind or anonymous volume
// only. The source must be a host path (absolute "/..." or relative "./...");
// a bare volume name is rejected — reference managed/external volumes via #self.
#HostMount: =~"^(/[^:]+|[/.][^:]*:/[^:]+(:[^:]+)?)$"

// --- Cross-unit reference handles (#self) ---
//
// Every unit exposes a `#self` field: a typed, decoratable handle that other
// units reference instead of a bare string. It carries a `_kind` discriminator
// (so a volume's #self cannot be placed in a network slot) and a `source` (the
// unit's #ref). A consuming field flattens `#self` to a string via `#rendered`,
// the same mechanism #SecretRef/secretStrings already use.

// #ServiceName is a systemd unit name, the branded type for [Unit] dependency
// fields (After/Requires/...). A value must end in a systemd unit suffix, so a
// podman ref (.container/.volume #self) or a typo'd bare word is rejected.
// A managed unit's #service and an external native unit's #ref are #ServiceNames.
#ServiceName: string & =~"\\.(service|socket|target|timer|path|mount|automount|device|swap|slice|scope)$"

// #RefSelf is the base handle for kinds referenced by a bare ref (network, pod,
// image, build, container, ...). It flattens to the plain #ref.
#RefSelf: {
	_kind:     string
	source:    string
	#rendered: source
}

// #VolumeMountOption is a podman volume mount flag (the ":options" field of a
// Volume= mount): a fixed enum of binary flags plus idmap, which may carry a
// custom mapping value. See podman-run(1) --volume.
#VolumeMountOption: "ro" | "rw" | "z" | "Z" | "U" | "chown" | "O" |
	"copy" | "nocopy" | "dev" | "nodev" | "exec" | "noexec" |
	"suid" | "nosuid" | "private" | "rprivate" | "shared" | "rshared" |
	"slave" | "rslave" | "unbindable" | "runbindable" |
	=~"^idmap(=.*)?$"

// #VolumeSelf is a volume's handle: a bare volume ref optionally decorated with
// a mount target and options, flattening to "source[:target[:opt,opt,...]]".
#VolumeSelf: {
	_kind:   "volume"
	source:  string
	target?: string
	options?: [...#VolumeMountOption]
	_optStr: strings.Join([if options != _|_ for o in options {o}], ",")
	#rendered: strings.Join(list.Concat([
		[source],
		[if target != _|_ {target}],
		[if _optStr != "" {_optStr}],
	]), ":")
}

// #VolumeMountRef is the Volume= destination form: a volume #self that must
// carry a mount target. A foreign kind's #self (different _kind) or a missing
// target is rejected here.
#VolumeMountRef: #VolumeSelf & {target: string}

// #MAC is a hardware address: six colon-separated hex octets.
#MAC: =~"^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$"

// #NetConnOptions are the connection options shared by a bridge network and a
// .network reference (podman-run --network): static addressing and aliases.
// The documented set is fixed, but netavark accepts more, so `passthrough`
// carries any not modeled here as raw key=value. Renders to "key=value,..."
// via _connStr (empty when nothing is set).
#NetConnOptions: {
	_prefix: string // what the options decorate: "bridge" or a .network ref
	alias?: [...string]   // repeatable -> alias=web,alias=app
	ip?:                  #IPv4
	ip6?:                 #IPv6
	mac?:                 #MAC
	interface_name?:      string
	host_interface_name?: string
	passthrough?: [...#KeyValue] // netavark options not modeled above
	_connStr: strings.Join(list.Concat([
		[if alias != _|_ for a in alias {"alias=\(a)"}],
		[if ip != _|_ {"ip=\(ip)"}],
		[if ip6 != _|_ {"ip6=\(ip6)"}],
		[if mac != _|_ {"mac=\(mac)"}],
		[if interface_name != _|_ {"interface_name=\(interface_name)"}],
		[if host_interface_name != _|_ {"host_interface_name=\(host_interface_name)"}],
		[if passthrough != _|_ for kv in passthrough {kv}],
	]), ",")
	// #rendered = "_prefix[:opt,opt,...]"; defined here (not in the embedding
	// struct) so the _connStr/_prefix references stay in #NetConnOptions's scope.
	#rendered: strings.Join(list.Concat([[_prefix], [if _connStr != "" {_connStr}]]), ":")
}

// Network= destination forms. A Network= field accepts a network's #self
// (optionally decorated with #NetConnOptions, rendering "name.network:ip=...,
// alias=..."), a container's #self (netns reuse via .container, no options), a
// raw mode (#NetworkMode), or bridge-with-options (#BridgeNet). A volume's #self
// (different _kind) is rejected.
#NetworkSelf: {
	_kind:   "network"
	source:  string
	_prefix: source
	#NetConnOptions
}
#ContainerSelf: #RefSelf & {_kind: "container"}

// #BridgeNet is bridge mode carrying connection options: {mode: "bridge", ip: ...}
// renders "bridge:ip=...". Bare "bridge" (no options) stays a plain string.
#BridgeNet: {
	mode:    "bridge"
	_prefix: "bridge"
	#NetConnOptions
}

// #NetworkMode is the strict raw Network= form: the nameless podman modes
// (host/none/private/bridge), bridge-with-options (#BridgeNet), and the
// open-ended pasta/slirp4netns/ns forms (pasta args and slirp4netns options are
// not enumerable; slirp4netns is legacy/deprecated, kept as a loose escape).
// Named references go through #self.
#NetworkMode: "host" | "none" | "private" | "bridge" | #BridgeNet |
	=~"^pasta(:.*)?$" | =~"^slirp4netns(:.*)?$" | =~"^ns:.+$"

// Pod= destination form: only a pod's #self (the spec accepts no raw values
// other than the .pod reference itself).
#PodSelf: #RefSelf & {_kind: "pod"}

// Device mapping for AddDevice= ([Container]).
// Form: [-]HOST-DEVICE[:CONTAINER-DEVICE][:PERMISSIONS] (podman-systemd.unit.5
// AddDevice=). A leading "-" adds the device only if it exists on the host;
// device paths are absolute; PERMISSIONS combines r (read), w (write), m (mknod).
#DeviceMapping: =~"^-?/[^:]+(:/[^:]+)?(:[rwm]{1,3})?$"

// Host mapping: hostname:ip
#HostMapping: =~"^[^:]+:.+$"

// --- Reusable building blocks (hidden) ---

// Bare non-negative integer string (e.g. "1024", "0").
_#bareInt: =~"^[0-9]+$"

// Integer with systemd byte-unit suffix (e.g. "64M", "2G", "512K").
// Base-1024 units: K, M, G, T, P, E.  See systemd.resource-control(5).
_#systemdByteSuffix: =~"^[0-9]+[KMGTPE]$"

// Integer with podman byte-unit suffix (e.g. "64m", "2g", "512k").
// Base-1024 units: b, k, m, g (case-insensitive).  See podman-run(1).
_#podmanByteSuffix: =~"^[0-9]+[bBkKmMgG]$"

// Percentage (e.g. "50%", "200%").
_#pct: =~"^[0-9]+%$"

// Systemd duration with time-unit suffix, including compound spans (e.g. "30s", "5min 20s").
// Units: us, ms, s, min, h, d, w.  See systemd.time(7).
_#systemdDuration: =~"^[0-9]+(us|ms|s|min|h|d|w)( [0-9]+(us|ms|s|min|h|d|w))*$"

// Go duration format, including fractional and compound spans (e.g. "30s",
// "1h30m", "500ms", "0.5s"). Units: ns, us, ms, s, m, h.  See Go
// time.ParseDuration. A leading sign is intentionally not allowed: a negative
// interval/timeout/delay is a config error for every field that uses this.
_#goDuration: =~"^([0-9]*\\.?[0-9]+(ns|us|ms|s|m|h))+$"

// --- Systemd value types ---

// Systemd byte size: bare integer, suffixed (K/M/G/T/P/E), percentage, or "infinity".
// See systemd.resource-control(5).
#Bytes: "infinity" | _#bareInt | _#systemdByteSuffix | _#pct

// Systemd time span: bare integer (seconds), suffixed/compound duration, or "infinity".
// See systemd.time(7).
#TimeSpan: "infinity" | _#bareInt | _#systemdDuration

// CPU time quota: percentage with "%" suffix (>100% allowed for multi-CPU).
// See systemd.resource-control(5).
#Percent: _#pct

// CPU weight: integer 1-10000, or "idle".
// See systemd.resource-control(5).
#CPUWeight: (int & >=1 & <=10000) | "idle"

// I/O weight: integer 1-10000.
// See systemd.resource-control(5).
#IOWeight: int & >=1 & <=10000

// Tasks limit: absolute count, percentage, or "infinity".
// See systemd.resource-control(5).
#TasksLimit: "infinity" | _#bareInt | _#pct

// Numeric resource limit (e.g. file descriptors, process count):
// single value, "infinity", or soft:hard colon pair.
// See systemd.exec(5).
#ResourceLimit: "infinity" | _#bareInt | =~"^([0-9]+|infinity):([0-9]+|infinity)$"

// Byte-valued resource limit (e.g. core dump size, locked memory):
// single value with optional suffix, "infinity", or soft:hard colon pair.
// See systemd.exec(5).
#ByteLimit: "infinity" | _#bareInt | _#systemdByteSuffix | =~"^([0-9]+[KMGTPE]?|infinity):([0-9]+[KMGTPE]?|infinity)$"

// POSIX signal, matching podman's ParseSignal: an optional "SIG" prefix, a bare
// name (TERM), a number (9), or a realtime signal (SIGRTMIN+3, RTMAX-1).
// See signal(7).
#Signal: =~"^(SIG)?([A-Z]+([+-][0-9]+)?|[0-9]+)$"

// #Capability is a Linux capability for AddCapability=/DropCapability=, in the
// conventional unprefixed uppercase form (NET_BIND_SERVICE), plus "ALL". The
// CAP_-prefixed regex arm is a forward-compat escape for caps newer than this
// list (40, per capabilities(7)); bump the enum when the kernel adds one.
#Capability: "ALL" |
	"AUDIT_CONTROL" | "AUDIT_READ" | "AUDIT_WRITE" | "BLOCK_SUSPEND" | "BPF" |
	"CHECKPOINT_RESTORE" | "CHOWN" | "DAC_OVERRIDE" | "DAC_READ_SEARCH" |
	"FOWNER" | "FSETID" | "IPC_LOCK" | "IPC_OWNER" | "KILL" | "LEASE" |
	"LINUX_IMMUTABLE" | "MAC_ADMIN" | "MAC_OVERRIDE" | "MKNOD" | "NET_ADMIN" |
	"NET_BIND_SERVICE" | "NET_BROADCAST" | "NET_RAW" | "PERFMON" | "SETFCAP" |
	"SETGID" | "SETPCAP" | "SETUID" | "SYS_ADMIN" | "SYS_BOOT" | "SYS_CHROOT" |
	"SYS_MODULE" | "SYS_NICE" | "SYS_PACCT" | "SYS_PTRACE" | "SYS_RAWIO" |
	"SYS_RESOURCE" | "SYS_TIME" | "SYS_TTY_CONFIG" | "SYSLOG" | "WAKE_ALARM" |
	=~"^CAP_[A-Z0-9_]+$"

// #Ulimit is a Ulimit= entry: "name=soft[:hard]" where name is a kernel RLIMIT
// type (lowercase, no RLIMIT_ prefix) and each limit is a number or -1
// (unlimited); or "host" to copy the host's limits. See setrlimit(2).
#Ulimit: "host" |
	=~"^(as|core|cpu|data|fsize|locks|memlock|msgqueue|nice|nofile|nproc|rss|rtprio|rttime|sigpending|stack)=(-1|[0-9]+)(:(-1|[0-9]+))?$"

// #TmpfsOption is one mount option for a typed #TmpfsSpec: a known kernel/podman
// tmpfs flag, or a key=value (size=, mode=, uid=, gid=, nr_blocks=, nr_inodes=,
// huge=). Not exhaustive (mpol= and exotic flags fall back to the raw-string form
// of #TmpfsMount); every token here is one the kernel actually accepts at mount.
// Verified against mm/shmem.c and tmpfs(5): huge= takes only never/always/
// within_size/advise at mount (deny/force are sysfs-only), and size=/nr_blocks=/
// nr_inodes= take memparse k/m/g/t/p/e suffixes (only size= also takes "%").
#TmpfsOption: "ro" | "rw" | "nosuid" | "suid" | "nodev" | "dev" |
	"noexec" | "exec" | "sync" | "async" | "dirsync" |
	"noatime" | "atime" | "nodiratime" | "diratime" |
	"relatime" | "norelatime" | "strictatime" | "nostrictatime" |
	"lazytime" | "nolazytime" | "nosymfollow" |
	"noswap" | "inode32" | "inode64" | "tmpcopyup" | "notmpcopyup" |
	=~"^size=[0-9]+([kKmMgGtTpPeE]|%)?$" |
	=~"^(nr_blocks|nr_inodes)=[0-9]+[kKmMgGtTpPeE]?$" |
	=~"^mode=[0-7]{1,4}$" |
	=~"^(uid|gid)=[0-9]+$" |
	=~"^huge=(never|always|within_size|advise)$"

// #TmpfsSpec is the typed form of a Tmpfs= entry: a container path plus a
// validated #TmpfsOption list, rendering to "path[:opt,opt,...]".
#TmpfsSpec: {
	path: =~"^/[^:]+$"
	options?: [...#TmpfsOption]
	_optStr: strings.Join([if options != _|_ for o in options {o}], ",")
	#rendered: strings.Join(list.Concat([
		[path],
		[if _optStr != "" {_optStr}],
	]), ":")
}

// #TmpfsMount is a Tmpfs= entry: either a raw string ("/run:rw,size=64m,mode=1777",
// the escape hatch for options #TmpfsOption does not model), or a typed #TmpfsSpec.
#TmpfsMount: =~"^/[^:]+(:.+)?$" | #TmpfsSpec

// systemd job mode for OnSuccessJobMode/OnFailureJobMode.
// See systemd.unit(5).
#JobMode: "replace" | "fail" | "replace-irreversibly" | "isolate" | "flush" | "ignore-dependencies" | "ignore-requirements"

// systemd emergency action for FailureAction/SuccessAction/StartLimitAction/
// JobTimeoutAction (values from systemd's emergency_action_table).
// See systemd.unit(5).
#EmergencyAction: "none" | "reboot" | "reboot-force" | "reboot-immediate" |
	"poweroff" | "poweroff-force" | "poweroff-immediate" |
	"exit" | "exit-force" | "soft-reboot" | "soft-reboot-force" |
	"kexec" | "kexec-force" | "halt" | "halt-force" | "halt-immediate"

// systemd unit garbage-collection mode for CollectMode. See systemd.unit(5).
#CollectMode: "inactive" | "inactive-or-failed"

// [Service] enums (values from systemd v257 string tables). See systemd.service(5).
#ServiceType:    "simple" | "exec" | "forking" | "oneshot" | "dbus" | "notify" | "notify-reload" | "idle"
#ServiceRestart: "no" | "on-success" | "on-failure" | "on-abnormal" | "on-watchdog" | "on-abort" | "always"
#KillMode:       "control-group" | "mixed" | "process" | "none"

#NotifyAccess: "none" | "main" | "exec" | "all"

// --- Podman value types ---

// Podman byte size: bare integer (bytes), or integer with b/k/m/g suffix (case-insensitive).
// Base-1024 units.  See podman-run(1) --memory.
#PodmanBytes: _#bareInt | _#podmanByteSuffix

// Go duration: used by podman for health checks, retry delays, etc.
// Supports ns, us, ms, s, m, h and compounds like "1h30m5s".
// See Go time.ParseDuration.
#GoDuration: _#goDuration

// --- Network types ---

// IPv4 address (e.g. "192.168.1.1").
#IPv4: =~"^([0-9]{1,3}\\.){3}[0-9]{1,3}$"

// IPv6 address (e.g. "::1", "fe80::1%eth0").
// Simplified pattern; full RFC 5952 validation deferred to runtime.
#IPv6: =~"^[0-9a-fA-F:]+(%.+)?$"

// IP address: IPv4 or IPv6.
#IPAddress: #IPv4 | #IPv6

// --- Podman enum types ---

// User namespace mode.  See podman-run(1) --userns.
#UserNS: "host" | "private" | "nomap" | =~"^keep-id(:.+)?$" | =~"^auto(:.+)?$" | =~"^ns:.+$" | =~"^container:.+$"

// --- Common types ---

// Podman secret reference for containers.
// Accepts either a raw string ("name,type=env,target=FOO") or a struct:
//   { name: "my-secret", type: "env", target: "MY_VAR" }
//
// Struct fields:
//   name:    secret name (required)
//   type:    "env" or "mount" (default: "mount")
//   target:  env var name or mount path (optional, defaults to secret name)
//   uid:     owner UID (mount type only, optional)
//   gid:     owner GID (mount type only, optional)
//   mode:    file permissions (mount type only, optional, e.g. "0400")
#SecretRef: {
	name:    string & !=""
	type?:   "env" | "mount"
	target?: string
	uid?:    int & >=0
	gid?:    int & >=0
	// File mode in octal, 3-4 digits, leading zero optional ("400", "0640",
	// "4000"). podman parses this with base-8 ParseUint, so "400" is valid.
	mode?: =~"^0?[0-7]{3,4}$"

	#rendered: strings.Join(list.Concat([
		[name],
		[if type != _|_ {"type=\(type)"}],
		[if target != _|_ {"target=\(target)"}],
		[if uid != _|_ {"uid=\(uid)"}],
		[if gid != _|_ {"gid=\(gid)"}],
		[if mode != _|_ {"mode=\(mode)"}],
	]), ",")
}

#SecretEntry: string | #SecretRef

// Container image reference (a raw image name; used by [Image]/[Volume] source).
#ImageRef: string & !=""

// Image= destination forms (strict). A raw image name (#ImageName, which must
// not look like a .image/.build ref), or a managed .image/.build via #self.
#ImageName: string & !="" & !~"\\.(image|build)$"
#ImageSelf: #RefSelf & {_kind: "image"}
#BuildSelf: #RefSelf & {_kind: "build"}

// #MountType is one of podman's --mount types.
#MountType: "bind" | "volume" | "tmpfs" | "image" | "devpts" | "glob" | "ramfs" | "artifact"

// #MountOption is a type-specific --mount option: a known boolean flag, or a
// known key=value. Options not modeled here go via #MountSpec.passthrough.
#MountOption: "ro" | "rw" | "U" | "chown" | "noswap" | "tmpcopyup" | "notmpcopyup" |
	"bind-nonrecursive" | "noatime" |
	=~"^bind-propagation=(r?private|r?shared|r?slave|r?unbindable)$" |
	=~"^relabel=(shared|private)$" |
	=~"^idmap(=.*)?$" |
	=~"^subpath=.+$" |
	=~"^(tmpfs-size|tmpfs-mode|uid|gid|mode|max|digest|title|name)=.+$"

// #MountRef builds a Mount= entry that references a managed volume or image by
// its #self handle, e.g.
//   Mount: [#MountRef & {ref: units.volumes.data.#self, destination: "/data"}]
// rendering "type=volume,source=app-data.volume,destination=/data".
#MountRef: {
	ref:         #VolumeSelf | #ImageSelf
	destination: string
	options?: [...#MountOption]
	#rendered: strings.Join(list.Concat([
		["type=\(ref._kind)", "source=\(ref.source)", "destination=\(destination)"],
		[if options != _|_ for o in options {o}],
	]), ",")
}

// #MountSpec is a structured raw Mount= entry: "type=TYPE,[source=SRC,]
// destination=DST[,opt,...]". type is one of #MountType, destination is required,
// options are validated (#MountOption), and passthrough carries any option not
// modeled. For a managed volume/image, prefer #MountRef (it brands the ref and
// wires the dependency).
#MountSpec: {
	type:        #MountType
	source?:     string
	destination: string
	options?: [...#MountOption]
	passthrough?: [...#KeyValue]
	#rendered: strings.Join(list.Concat([
		["type=\(type)"],
		[if source != _|_ {"source=\(source)"}],
		["destination=\(destination)"],
		[if options != _|_ for o in options {o}],
		[if passthrough != _|_ for kv in passthrough {kv}],
	]), ",")
}

// UID/GID mapping for UIDMap=/GIDMap= ([Container]/[Pod]).
// Form: [flags]container_id:from_id[:amount] (podman-run --uidmap/--gidmap).
// flags is any combination of "+" (extend previous mapping) and "u"/"g" (apply
// to UIDs/GIDs only); from_id may be "@"-prefixed to reference a host id through
// the intermediate namespace; amount is optional and defaults to 1.
#IDMap: =~"^[+ug]*[0-9]+:@?[0-9]+(:[0-9]+)?$"

// Unix file mode in octal notation (e.g. "0755", "0644")
#FileMode: =~"^0[0-7]{3}$"

// Pull policy
#PullPolicy: "always" | "missing" | "never" | "newer"

// Auto-update policy
#AutoUpdatePolicy: "registry" | "local"

// Log driver
#LogDriver: "k8s-file" | "journald" | "none" | "passthrough" | string

// Network driver
#NetworkDriver: "bridge" | "macvlan" | "ipvlan"

// IPAM driver
#IPAMDriver: "host-local" | "dhcp" | "none"

// Cgroup mode
#CgroupsMode: "split" | "no-conmon" | "enabled" | "disabled"

// Notify mode
#NotifyMode: bool | "healthy"

// Health on-failure action
#HealthOnFailure: "none" | "kill" | "restart" | "stop"

// Exit code propagation
#ExitCodePropagation: "all" | "any" | "none"

// Pod exit policy
#PodExitPolicy: "stop" | "continue"

// Systemd common sections that can appear in any unit file (#UnitSection,
// #ServiceSection, #InstallSection) are generated from systemd's own parser
// table into systemd_sections.gen.cue (see tools/gen-systemd-sections).

// Quadlet-specific section
#QuadletSection: {
	DefaultDependencies?: bool
}
