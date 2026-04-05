package creidhne

import (
	"list"
	"strings"
)

// Common types used across all Quadlet unit definitions.

// Port mapping: ip:hostPort:containerPort/protocol
#PortMapping: =~"^([0-9.]*:)?[0-9]+(-[0-9]+)?(:[0-9]+(-[0-9]+)?)?(/(tcp|udp|sctp))?$" | =~"^[0-9]+(-[0-9]+)?$"

// Key=Value pair for labels, annotations, environment variables
#KeyValue: =~"^[^=]+=.*$"

// CIDR notation
#CIDR: =~"^[0-9a-fA-F:.]+/[0-9]+$"

// IP range: CIDR notation or startIP-endIP
#IPRange: #CIDR | =~"^[0-9a-fA-F:.]+-[0-9a-fA-F:.]+$"

// Volume mount: [SOURCE:]DEST[:OPTIONS]
#VolumeMount: string

// Device mapping: HOST[:CONTAINER[:PERMS]]
#DeviceMapping: string

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

// Go duration format, including compounds (e.g. "30s", "1h30m", "500ms").
// Units: ns, us, ms, s, m, h.  See Go time.ParseDuration.
_#goDuration: =~"^[0-9]+(ns|us|ms|s|m|h)([0-9]+(ns|us|ms|s|m|h))*$"

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

// POSIX signal name (e.g. SIGTERM, SIGKILL, SIGHUP).
// See signal(7).
#Signal: =~"^SIG[A-Z0-9]+$"

// systemd job mode for OnSuccessJobMode/OnFailureJobMode.
// See systemd.unit(5).
#JobMode: "replace" | "fail" | "replace-irreversibly" | "isolate" | "flush" | "ignore-dependencies" | "ignore-requirements"

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
	mode?:   =~"^0[0-7]{3,4}$"

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

// UID/GID mapping format
#IDMap: string

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

// Systemd common sections that can appear in any unit file.

#UnitSection: {
	Description?:   string
	Documentation?: string
	Requires?: [...string]
	After?: [...string]
	Before?: [...string]
	Wants?: [...string]
	BindsTo?: [...string]
	PartOf?: [...string]
	Conflicts?: [...string]
	Condition?: [...string]
	Assert?: [...string]
	SourcePath?:            string
	StopWhenUnneeded?:      bool
	RefuseManualStart?:     bool
	RefuseManualStop?:      bool
	AllowIsolate?:          bool
	IgnoreOnIsolate?:       bool
	OnSuccess?:             string
	OnFailure?:             string
	OnSuccessJobMode?:      #JobMode
	OnFailureJobMode?:      #JobMode
	StartLimitIntervalSec?: #TimeSpan
	StartLimitBurst?:       int & >=0
}

#ServiceSection: {
	Type?:             "simple" | "exec" | "forking" | "oneshot" | "dbus" | "notify" | "idle"
	Restart?:          "no" | "on-success" | "on-failure" | "on-abnormal" | "on-watchdog" | "on-abort" | "always"
	RestartSec?:       #TimeSpan
	TimeoutStartSec?:  #TimeSpan
	TimeoutStopSec?:   #TimeSpan
	TimeoutSec?:       #TimeSpan
	WatchdogSec?:      #TimeSpan
	ExecStartPre?: [...string]
	ExecStartPost?: [...string]
	ExecStop?: [...string]
	ExecStopPost?: [...string]
	ExecReload?: [...string]
	RemainAfterExit?:  bool
	KillMode?:         "control-group" | "mixed" | "process" | "none"
	KillSignal?:       #Signal
	NotifyAccess?:     "none" | "main" | "exec" | "all"
	RestartPreventExitStatus?: string
	SuccessExitStatus?: string
	Environment?: [...string]
	EnvironmentFile?: [...string]
	WorkingDirectory?: string

	// Resource controls (systemd.resource-control(5))
	MemoryMax?:    #Bytes
	MemoryHigh?:   #Bytes
	MemoryLow?:    #Bytes
	MemoryMin?:    #Bytes
	CPUQuota?:     #Percent
	CPUWeight?:    #CPUWeight
	IOWeight?:     #IOWeight
	TasksMax?:     #TasksLimit

	// Process resource limits (systemd.exec(5))
	LimitNOFILE?:  #ResourceLimit
	LimitNPROC?:   #ResourceLimit
	LimitCORE?:    #ByteLimit
	LimitMEMLOCK?: #ByteLimit
}

#InstallSection: {
	WantedBy?: [...string]
	RequiredBy?: [...string]
	UpheldBy?: [...string]
	Alias?: [...string]
	DefaultInstance?: string
}

// Quadlet-specific section
#QuadletSection: {
	DefaultDependencies?: bool
}
