package creidhne

import (
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
#ServiceType: "simple" | "exec" | "forking" | "oneshot" | "dbus" | "notify" | "notify-reload" | "idle"
#ServiceRestart: "no" | "on-success" | "on-failure" | "on-abnormal" | "on-watchdog" | "on-abort" | "always"
#KillMode: "control-group" | "mixed" | "process" | "none"
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

	_rendered: strings.Join(list.Concat([
		[name],
		[ if type != _|_ {"type=\(type)"}],
		[ if target != _|_ {"target=\(target)"}],
		[ if uid != _|_ {"uid=\(uid)"}],
		[ if gid != _|_ {"gid=\(gid)"}],
		[ if mode != _|_ {"mode=\(mode)"}],
	]), ",")
}

#SecretEntry: string | #SecretRef

// Container image reference
#ImageRef: string & !=""

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
