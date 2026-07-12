package importer

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
)

var unitNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// mapProject converts the loaded compose project into the emission model.
func mapProject(p *types.Project, opts Options) (*model, error) {
	m := &model{quadletName: p.Name, secretKeys: newKeymap()}
	m.env = newEnvSet(m.warnf)
	m.env.resolved = opts.ResolveEnv
	if !unitNameRe.MatchString(m.quadletName) {
		return nil, fmt.Errorf("project name %q is not a valid quadlet name (want %s); pass --name", m.quadletName, unitNameRe)
	}

	serviceNames := make([]string, 0, len(p.Services))
	for name := range p.Services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)
	for _, name := range p.DisabledServices {
		m.warnf("service %s: disabled by profiles; not converted", name.Name)
	}

	volumeKeys := sortedKeys(p.Volumes)
	networkKeys := sortedKeys(p.Networks)
	buildCount := 0
	for _, name := range serviceNames {
		if p.Services[name].Build != nil {
			buildCount++
		}
	}
	r := refs{
		singularContainer: len(serviceNames) == 1,
		singularVolume:    len(volumeKeys) == 1 && len(serviceNames) == 1,
		singularNetwork:   len(networkKeys) == 1 && len(serviceNames) == 1,
		singularBuild:     buildCount == 1 && len(serviceNames) == 1,
		containers:        newKeymap(),
		volumes:           newKeymap(),
		networks:          newKeymap(),
		builds:            newKeymap(),
	}
	// Assign keys in sorted order so collision fallbacks are deterministic.
	for _, name := range serviceNames {
		r.containers.key(name)
		r.builds.key(name)
	}
	for _, key := range volumeKeys {
		r.volumes.key(key)
	}
	for _, key := range networkKeys {
		r.networks.key(key)
	}
	m.forms = r

	mapSecrets(m, p)
	names := &nameNotes{fresh: !opts.PreserveNames}
	for _, key := range volumeKeys {
		mapVolume(m, r.volumes, names, key, p.Volumes[key])
	}
	for _, key := range networkKeys {
		mapNetwork(m, r.networks, names, key, p.Networks[key])
	}
	names.emit(m)

	// notifyHealthy collects services that others depend on with
	// condition: service_healthy; their containers get Notify=healthy.
	notifyHealthy := map[string]bool{}
	for _, name := range serviceNames {
		svc := p.Services[name]
		mapService(m, p, r, name, svc, notifyHealthy)
	}
	// Services with a healthcheck already carry the full notify wiring from
	// mapServiceHealthcheck. A checkless dependency must NOT get
	// Notify=healthy (READY would never arrive and the start job would hang
	// to its timeout), so a service_healthy dependent on it only warns.
	for _, name := range serviceNames {
		if !notifyHealthy[name] {
			continue
		}
		if c := findUnit(m.containers, r.containers.key(name)); c != nil && !hasField(c, "Container", "HealthCmd") {
			m.warnf("service %s: a dependent uses condition: service_healthy but it defines no healthcheck; the dependency waits for container start only", name)
		}
	}

	if len(p.Configs) > 0 {
		for _, k := range sortedKeys(p.Configs) {
			m.warnf("configs.%s: compose configs have no quadlet equivalent; mount the file explicitly or convert it to a secret", k)
		}
	}
	return m, nil
}

func findUnit(units []unitDef, key string) *unitDef {
	for i := range units {
		if units[i].key == key {
			return &units[i]
		}
	}
	return nil
}

func hasField(u *unitDef, sec, key string) bool {
	for _, s := range u.sections {
		if s.name != sec {
			continue
		}
		for _, f := range s.fields {
			if f.k == key {
				return true
			}
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- secrets ---

func mapSecrets(m *model, p *types.Project) {
	for _, key := range sortedKeys(p.Secrets) {
		s := p.Secrets[key]
		cueKey := m.secretKeys.key(key)
		d := secretDecl{key: cueKey, file: s.File, external: bool(s.External)}
		switch {
		case s.Name != "" && s.Name != cueKey:
			d.name = s.Name
		case cueKey != key:
			// The registry key had to be sanitized into an identifier; keep
			// the podman secret name as the compose spelling.
			d.name = key
		}
		podmanName := cueKey
		if d.name != "" {
			podmanName = d.name
		}
		switch {
		case bool(s.External):
			m.stepf("secret %s: external; verify it exists (crei secrets list)", podmanName)
		case s.File != "":
			m.stepf("secret %s: load its value from %s (podman secret create %s %s)", podmanName, s.File, podmanName, s.File)
		case s.Environment != "":
			m.warnf("secrets.%s: environment-sourced compose secrets are not supported; create the podman secret manually", key)
			m.stepf("secret %s: create it (crei secrets create %s)", podmanName, podmanName)
		case s.Content != "":
			// Inline secret material stays out of the emitted CUE and report.
			m.warnf("secrets.%s: inline content is not imported (secret material does not belong in CUE)", key)
			m.stepf("secret %s: create it with the inline value from the compose file (crei secrets create %s)", podmanName, podmanName)
		default:
			m.stepf("secret %s: create it (crei secrets create %s)", podmanName, podmanName)
		}
		if s.Driver != "" || s.TemplateDriver != "" {
			m.warnf("secrets.%s: driver/template_driver are swarm-only; dropped", key)
		}
		m.secrets = append(m.secrets, d)
	}
}

// --- volumes / networks ---

// nameNotes aggregates runtime-name decisions into report notifications.
type nameNotes struct {
	fresh                bool
	preservedV, freshV   []string
	preservedN           []string
	externalV, externalN []string
}

func (n *nameNotes) preservedVolume(name string)  { n.preservedV = append(n.preservedV, name) }
func (n *nameNotes) preservedNetwork(name string) { n.preservedN = append(n.preservedN, name) }
func (n *nameNotes) freshVolume(name string)      { n.freshV = append(n.freshV, name) }
func (n *nameNotes) externalVolume(name string)   { n.externalV = append(n.externalV, name) }
func (n *nameNotes) externalNetwork(name string)  { n.externalN = append(n.externalN, name) }

func (n *nameNotes) emit(m *model) {
	if len(n.preservedV) > 0 {
		m.notef("VolumeName preserves the compose-era volumes (%s) so existing data is reused", strings.Join(n.preservedV, ", "))
	}
	if len(n.preservedN) > 0 {
		m.notef("NetworkName preserves the compose-era networks (%s)", strings.Join(n.preservedN, ", "))
	}
	if len(n.freshV) > 0 {
		m.notef("volumes get fresh systemd-* names; migrating an existing deployment? --preserve-names reuses the compose-era volumes (%s)", strings.Join(n.freshV, ", "))
	}
	if len(n.externalV) > 0 {
		m.notef("adopting existing external volumes by name: %s", strings.Join(n.externalV, ", "))
	}
	if len(n.externalN) > 0 {
		m.notef("adopting existing external networks by name: %s", strings.Join(n.externalN, ", "))
	}
}

func mapVolume(m *model, km *keymap, names *nameNotes, key string, v types.VolumeConfig) {
	u := newUnit(km, key)
	// Preserve the runtime volume name (compose: <project>_<key>, or the
	// explicit name), so existing data is reused instead of a fresh
	// systemd-* volume appearing empty. Externals are always adopted by name.
	switch {
	case v.Name != "" && bool(v.External):
		u.add("Volume", "VolumeName", strconv.Quote(v.Name))
		names.externalVolume(v.Name)
	case v.Name != "" && !names.fresh:
		u.add("Volume", "VolumeName", strconv.Quote(v.Name))
		names.preservedVolume(v.Name)
	case v.Name != "":
		names.freshVolume(v.Name)
	}
	if v.Driver != "" && v.Driver != "local" {
		u.add("Volume", "Driver", strconv.Quote(v.Driver))
	}
	for _, k := range sortedKeys(v.DriverOpts) {
		u.addList("Volume", "Options", strconv.Quote(k+"="+v.DriverOpts[k]))
	}
	u.addList("Volume", "Label", labelList(m, v.Labels)...)
	m.volumes = append(m.volumes, u)
}

func mapNetwork(m *model, km *keymap, names *nameNotes, key string, n types.NetworkConfig) {
	u := newUnit(km, key)
	switch {
	case n.Name != "" && bool(n.External):
		u.add("Network", "NetworkName", strconv.Quote(n.Name))
		names.externalNetwork(n.Name)
	case n.Name != "" && !names.fresh:
		u.add("Network", "NetworkName", strconv.Quote(n.Name))
		names.preservedNetwork(n.Name)
	}
	if n.Driver != "" && n.Driver != "bridge" {
		u.add("Network", "Driver", strconv.Quote(n.Driver))
	}
	if n.Internal {
		u.add("Network", "Internal", "true")
	}
	if n.EnableIPv6 != nil && *n.EnableIPv6 {
		u.add("Network", "IPv6", "true")
	}
	var subnets, gateways, ipranges []string
	for _, cfg := range n.Ipam.Config {
		if cfg.Subnet != "" {
			subnets = append(subnets, strconv.Quote(cfg.Subnet))
		}
		if cfg.Gateway != "" {
			gateways = append(gateways, strconv.Quote(cfg.Gateway))
		}
		if cfg.IPRange != "" {
			ipranges = append(ipranges, strconv.Quote(cfg.IPRange))
		}
		if len(cfg.AuxiliaryAddresses) > 0 {
			m.warnf("networks.%s: ipam aux_addresses are not supported; dropped", key)
		}
	}
	if n.Ipam.Driver != "" {
		u.add("Network", "IPAMDriver", strconv.Quote(n.Ipam.Driver))
	}
	u.addList("Network", "Subnet", subnets...)
	u.addList("Network", "Gateway", gateways...)
	u.addList("Network", "IPRange", ipranges...)
	for _, k := range sortedKeys(n.DriverOpts) {
		u.addList("Network", "Options", strconv.Quote(k+"="+n.DriverOpts[k]))
	}
	u.addList("Network", "Label", labelList(m, n.Labels)...)
	if n.Attachable {
		m.warnf("networks.%s: attachable is docker-specific; podman networks are always attachable", key)
	}
	if n.EnableIPv4 != nil && !*n.EnableIPv4 {
		m.warnf("networks.%s: enable_ipv4: false has no quadlet equivalent; dropped", key)
	}
	if len(n.Ipam.Options) > 0 {
		m.warnf("networks.%s: ipam driver options are not supported; dropped", key)
	}
	m.networks = append(m.networks, u)
}

// --- services ---

func mapService(m *model, p *types.Project, r refs, name string, svc types.ServiceConfig, notifyHealthy map[string]bool) {
	u := newUnit(r.containers, name)

	// Image / build.
	switch {
	case svc.Build != nil:
		mapBuild(m, p, r, name, svc)
		u.add("Container", "Image", r.buildSelf(name))
	case svc.Image != "":
		u.add("Container", "Image", m.env.rewrite(svc.Image))
	default:
		m.warnf("service %s: neither image nor build; emitted without Image (validate will fail until set)", name)
	}
	if svc.ContainerName != "" {
		u.add("Container", "ContainerName", m.env.rewrite(svc.ContainerName))
	}
	if len(svc.Entrypoint) > 0 {
		u.add("Container", "Entrypoint", m.env.rewrite(jsonArray(svc.Entrypoint)))
	}
	if len(svc.Command) > 0 {
		u.add("Container", "Exec", m.env.rewrite(shellJoin(svc.Command)))
	}
	if svc.WorkingDir != "" {
		u.add("Container", "WorkingDir", m.env.rewrite(svc.WorkingDir))
	}
	if svc.User != "" {
		user, group, ok := strings.Cut(svc.User, ":")
		u.add("Container", "User", m.env.rewrite(user))
		if ok {
			u.add("Container", "Group", m.env.rewrite(group))
		}
	}
	if svc.Init != nil && *svc.Init {
		u.add("Container", "RunInit", "true")
	}
	if len(svc.GroupAdd) > 0 {
		u.addList("Container", "GroupAdd", quoteAll(svc.GroupAdd)...)
	}

	// Environment.
	if len(svc.Environment) > 0 {
		var envs []string
		for _, k := range sortedKeys(svc.Environment) {
			v := svc.Environment[k]
			if v == nil {
				// compose pass-through from the host: lift to a required var.
				envs = append(envs, `"`+quoteInner(k+"=")+`\(`+m.env.record(k, "", false)+`)"`)
				continue
			}
			envs = append(envs, m.env.rewrite(k+"="+*v))
		}
		u.addList("Container", "Environment", envs...)
	}
	if len(svc.EnvFiles) > 0 {
		var files []string
		for _, f := range svc.EnvFiles {
			files = append(files, m.env.rewrite(f.Path))
			if !bool(f.Required) {
				m.warnf("service %s: env_file %s is marked optional; podman has no optional env-file semantics", name, f.Path)
			}
		}
		u.addList("Container", "EnvironmentFile", files...)
	}

	mapServiceNetworks(m, r, &u, name, svc)
	mapServicePorts(m, &u, name, svc)
	mapServiceVolumes(m, p, r, &u, name, svc)
	mapServiceRuntimeKnobs(m, &u, name, svc)
	mapServiceHealthcheck(m, &u, name, svc)
	mapServiceSecrets(m, p, &u, name, svc)
	mapServiceResources(m, &u, name, svc)
	mapDependsOn(m, r, &u, name, svc, notifyHealthy)
	warnUnsupported(m, &u, name, svc)

	m.containers = append(m.containers, u)
}

func mapBuild(m *model, p *types.Project, r refs, name string, svc types.ServiceConfig) {
	b := svc.Build
	u := newUnit(r.builds, name)
	if b.DockerfileInline != "" {
		u.add("", "ContainerFile", strconv.Quote(b.DockerfileInline))
		if b.Context != "" && b.Context != "." {
			m.warnf("service %s: build.dockerfile_inline with a context directory: the context is not carried into the unit", name)
		}
	} else {
		ctx := b.Context
		if ctx == "" {
			ctx = "."
		}
		u.add("Build", "SetWorkingDirectory", m.env.rewrite(ctx))
		if b.Dockerfile != "" && b.Dockerfile != "Dockerfile" {
			u.add("Build", "File", m.env.rewrite(b.Dockerfile))
		}
	}
	tags := []string{}
	if svc.Image != "" {
		tags = append(tags, m.env.rewrite(svc.Image))
	}
	for _, t := range b.Tags {
		tags = append(tags, m.env.rewrite(t))
	}
	u.addList("Build", "ImageTag", tags...)
	if b.Target != "" {
		u.add("Build", "Target", m.env.rewrite(b.Target))
	}
	switch b.Network {
	case "":
	case "host", "none":
		u.addList("Build", "Network", strconv.Quote(b.Network))
	default:
		u.addList("Build", "Network", r.networkSelf(b.Network))
	}
	var buildSecrets []string
	for _, s := range b.Secrets {
		id := s.Source
		if s.Target != "" {
			id = s.Target
		}
		src, ok := p.Secrets[s.Source]
		switch {
		case ok && src.File != "":
			buildSecrets = append(buildSecrets, m.env.rewrite("id="+id+",src="+src.File))
		case ok && src.Environment != "":
			buildSecrets = append(buildSecrets, strconv.Quote("id="+id+",env="+src.Environment))
		default:
			m.warnf("service %s: build secret %q has no file/environment source; not mapped", name, s.Source)
		}
	}
	u.addList("Build", "Secret", buildSecrets...)
	var args []string
	for _, k := range sortedKeys(b.Args) {
		if v := b.Args[k]; v != nil {
			args = append(args, m.env.rewrite(k+"="+*v))
		} else {
			args = append(args, `"`+quoteInner(k+"=")+`\(`+m.env.record(k, "", false)+`)"`)
		}
	}
	if len(args) > 0 {
		u.addList("Build", "BuildArg", args...)
	}
	if b.Pull {
		u.add("Build", "Pull", `"always"`)
	}
	u.addList("Build", "Label", labelList(m, b.Labels)...)
	for _, skipped := range []struct {
		set  bool
		what string
	}{
		{len(b.CacheFrom) > 0, "cache_from"},
		{len(b.CacheTo) > 0, "cache_to"},
		{len(b.AdditionalContexts) > 0, "additional_contexts"},
		{b.NoCache, "no_cache"},
		{len(b.SSH) > 0, "ssh"},
	} {
		if skipped.set {
			m.warnf("service %s: build.%s has no quadlet equivalent; dropped", name, skipped.what)
		}
	}
	m.builds = append(m.builds, u)
}

func mapServiceNetworks(m *model, r refs, u *unitDef, name string, svc types.ServiceConfig) {
	if svc.NetworkMode != "" {
		mode := svc.NetworkMode
		switch {
		case strings.HasPrefix(mode, "service:"):
			u.addList("Container", "Network", r.containerSelf(strings.TrimPrefix(mode, "service:")))
		case strings.HasPrefix(mode, "container:"):
			m.warnf("service %s: network_mode container:... references a container outside this project; not supported, dropped", name)
		case mode == "host" || mode == "none" || mode == "bridge" || mode == "private":
			u.addList("Container", "Network", strconv.Quote(mode))
		default:
			m.warnf("service %s: network_mode %q not supported; dropped", name, mode)
		}
		return
	}
	// Service-level mac_address is deprecated in favor of the per-network
	// attachment field; fold it into a sole attachment, warn otherwise.
	svcMAC := svc.MacAddress
	if svcMAC != "" && len(svc.Networks) != 1 {
		m.warnf("service %s: mac_address with %d network attachments is ambiguous; set mac per network", name, len(svc.Networks))
		svcMAC = ""
	}
	var nets []string
	for _, key := range sortedKeys(svc.Networks) {
		cfg := svc.Networks[key]
		ref := r.networkSelf(key)
		var opts []string
		if cfg == nil && svcMAC != "" {
			cfg = &types.ServiceNetworkConfig{}
		}
		if cfg != nil {
			if cfg.MacAddress == "" && svcMAC != "" {
				cfg.MacAddress = svcMAC
			}
			for _, a := range cfg.Aliases {
				opts = append(opts, "alias: "+listExpr([]string{m.env.rewrite(a)}))
			}
			if cfg.Ipv4Address != "" {
				opts = append(opts, "ip: "+m.env.rewrite(cfg.Ipv4Address))
			}
			if cfg.Ipv6Address != "" {
				opts = append(opts, "ip6: "+m.env.rewrite(cfg.Ipv6Address))
			}
			if cfg.MacAddress != "" {
				opts = append(opts, "mac: "+m.env.rewrite(cfg.MacAddress))
			}
			if cfg.InterfaceName != "" {
				opts = append(opts, "interface_name: "+m.env.rewrite(cfg.InterfaceName))
			}
			if len(cfg.LinkLocalIPs) > 0 || cfg.GatewayPriority != 0 || len(cfg.DriverOpts) > 0 {
				m.warnf("service %s: network %s attachment options (link_local_ips/gw_priority/driver_opts) not supported; dropped", name, key)
			}
		}
		if len(opts) > 0 {
			// Aliases may repeat: merge alias lists into one.
			opts = mergeAliasOpts(opts)
			ref += " & {" + strings.Join(opts, ", ") + "}"
		}
		nets = append(nets, ref)
	}
	u.addList("Container", "Network", nets...)
}

func mergeAliasOpts(opts []string) []string {
	var aliases []string
	out := opts[:0]
	for _, o := range opts {
		if v, ok := strings.CutPrefix(o, "alias: ["); ok {
			aliases = append(aliases, strings.TrimSuffix(v, "]"))
			continue
		}
		out = append(out, o)
	}
	if len(aliases) > 0 {
		out = append([]string{"alias: [" + strings.Join(aliases, ", ") + "]"}, out...)
	}
	return out
}

func mapServicePorts(m *model, u *unitDef, name string, svc types.ServiceConfig) {
	var ports []string
	for _, pc := range svc.Ports {
		if pc.Mode != "" && pc.Mode != "ingress" && pc.Mode != "host" {
			m.warnf("service %s: port mode %q not supported; emitted as a plain mapping", name, pc.Mode)
		}
		var b strings.Builder
		if pc.HostIP != "" {
			b.WriteString(pc.HostIP + ":")
		}
		if pc.Published != "" {
			b.WriteString(pc.Published + ":")
		} else if pc.HostIP != "" {
			b.WriteString(":")
		}
		b.WriteString(strconv.FormatUint(uint64(pc.Target), 10))
		if pc.Protocol != "" && pc.Protocol != "tcp" {
			b.WriteString("/" + pc.Protocol)
		}
		ports = append(ports, m.env.rewrite(b.String()))
	}
	u.addList("Container", "PublishPort", ports...)
	if len(svc.Expose) > 0 {
		var exp []string
		for _, e := range svc.Expose {
			exp = append(exp, m.env.rewrite(e))
		}
		u.addList("Container", "ExposeHostPort", exp...)
	}
}

func mapServiceVolumes(m *model, p *types.Project, r refs, u *unitDef, name string, svc types.ServiceConfig) {
	var mounts, tmpfs []string
	for _, v := range svc.Volumes {
		switch v.Type {
		case "volume":
			ref := r.volumeSelf(v.Source)
			if _, ok := p.Volumes[v.Source]; !ok {
				m.warnf("service %s: volume %q is not declared top-level; declared implicitly", name, v.Source)
				m.volumes = append(m.volumes, newUnit(r.volumes, v.Source))
			}
			opts := []string{"target: " + strconv.Quote(v.Target)}
			var mountOpts []string
			if v.ReadOnly {
				mountOpts = append(mountOpts, `"ro"`)
			}
			if v.Volume != nil && v.Volume.NoCopy {
				mountOpts = append(mountOpts, `"nocopy"`)
			}
			if v.Volume != nil && v.Volume.Subpath != "" {
				m.warnf("service %s: volume %s subpath is not supported by Volume=; mount the parent or use a bind", name, v.Source)
			}
			if v.Volume != nil && len(v.Volume.Labels) > 0 {
				m.warnf("service %s: per-mount volume labels are not supported; dropped", name)
			}
			if len(mountOpts) > 0 {
				opts = append(opts, "options: "+listExpr(mountOpts))
			}
			mounts = append(mounts, ref+" & {"+strings.Join(opts, ", ")+"}")
		case "bind":
			var o []string
			if v.ReadOnly {
				o = append(o, "ro")
			}
			if v.Bind != nil {
				if v.Bind.SELinux != "" {
					o = append(o, string(v.Bind.SELinux))
				}
				if v.Bind.Propagation != "" {
					o = append(o, string(v.Bind.Propagation))
				}
			}
			s := v.Source + ":" + v.Target
			if len(o) > 0 {
				s += ":" + strings.Join(o, ",")
			}
			mounts = append(mounts, m.env.rewrite(s))
		case "tmpfs":
			var o []string
			if v.Tmpfs != nil {
				if v.Tmpfs.Size > 0 {
					o = append(o, fmt.Sprintf("%q", fmt.Sprintf("size=%d", int64(v.Tmpfs.Size))))
				}
				if v.Tmpfs.Mode > 0 {
					o = append(o, fmt.Sprintf("%q", fmt.Sprintf("mode=%o", v.Tmpfs.Mode)))
				}
			}
			entry := "{path: " + strconv.Quote(v.Target)
			if len(o) > 0 {
				entry += ", options: " + listExpr(o)
			}
			entry += "}"
			tmpfs = append(tmpfs, entry)
		default:
			m.warnf("service %s: volume type %q not supported; dropped", name, v.Type)
		}
	}
	for _, t := range svc.Tmpfs {
		tmpfs = append(tmpfs, m.env.rewrite(t))
	}
	u.addList("Container", "Volume", mounts...)
	u.addList("Container", "Tmpfs", tmpfs...)
}

func mapServiceRuntimeKnobs(m *model, u *unitDef, name string, svc types.ServiceConfig) {
	if svc.Hostname != "" {
		u.add("Container", "HostName", m.env.rewrite(svc.Hostname))
	}
	if len(svc.DNS) > 0 {
		u.addList("Container", "DNS", quoteAll(svc.DNS)...)
	}
	if len(svc.DNSOpts) > 0 {
		u.addList("Container", "DNSOption", quoteAll(svc.DNSOpts)...)
	}
	if len(svc.DNSSearch) > 0 {
		u.addList("Container", "DNSSearch", quoteAll(svc.DNSSearch)...)
	}
	if len(svc.ExtraHosts) > 0 {
		var hosts []string
		for _, h := range sortedKeys(svc.ExtraHosts) {
			for _, ip := range svc.ExtraHosts[h] {
				hosts = append(hosts, strconv.Quote(h+":"+ip))
			}
		}
		u.addList("Container", "AddHost", hosts...)
	}
	if len(svc.CapAdd) > 0 {
		u.addList("Container", "AddCapability", quoteAll(svc.CapAdd)...)
	}
	if len(svc.CapDrop) > 0 {
		u.addList("Container", "DropCapability", quoteAll(svc.CapDrop)...)
	}
	for _, so := range svc.SecurityOpt {
		switch {
		case so == "no-new-privileges" || so == "no-new-privileges:true":
			u.add("Container", "NoNewPrivileges", "true")
		case so == "label=disable" || so == "label:disable":
			u.add("Container", "SecurityLabelDisable", "true")
		case strings.HasPrefix(so, "seccomp="):
			u.add("Container", "SeccompProfile", strconv.Quote(strings.TrimPrefix(so, "seccomp=")))
		case strings.HasPrefix(so, "apparmor="):
			u.add("Container", "AppArmor", strconv.Quote(strings.TrimPrefix(so, "apparmor=")))
		default:
			m.warnf("service %s: security_opt %q not supported; dropped", name, so)
		}
	}
	if svc.UserNSMode != "" {
		// podman --userns modes pass through; anything else is docker-specific.
		switch {
		case svc.UserNSMode == "host" || svc.UserNSMode == "private" || svc.UserNSMode == "nomap",
			strings.HasPrefix(svc.UserNSMode, "keep-id"),
			strings.HasPrefix(svc.UserNSMode, "auto"),
			strings.HasPrefix(svc.UserNSMode, "ns:"),
			strings.HasPrefix(svc.UserNSMode, "container:"):
			u.add("Container", "UserNS", strconv.Quote(svc.UserNSMode))
		default:
			m.warnf("service %s: userns_mode %q is not a podman --userns mode; dropped", name, svc.UserNSMode)
			u.dropComment("userns_mode", svc.UserNSMode)
		}
	}
	if svc.Privileged {
		u.addList("Container", "PodmanArgs", `"--privileged"`)
		m.warnf("service %s: privileged mapped to PodmanArgs=--privileged; consider explicit capabilities instead", name)
	}
	if svc.ReadOnly {
		u.add("Container", "ReadOnly", "true")
	}
	if len(svc.Devices) > 0 {
		var devs []string
		for _, d := range svc.Devices {
			s := d.Source
			if d.Target != "" {
				s += ":" + d.Target
			}
			if d.Permissions != "" {
				s += ":" + d.Permissions
			}
			devs = append(devs, strconv.Quote(s))
		}
		u.addList("Container", "AddDevice", devs...)
	}
	if len(svc.Sysctls) > 0 {
		var sys []string
		for _, k := range sortedKeys(svc.Sysctls) {
			sys = append(sys, strconv.Quote(k+"="+svc.Sysctls[k]))
		}
		u.addList("Container", "Sysctl", sys...)
	}
	if svc.ShmSize > 0 {
		// #PodmanBytes is a string pattern, not an int.
		u.add("Container", "ShmSize", strconv.Quote(strconv.FormatInt(int64(svc.ShmSize), 10)))
	}
	if len(svc.Ulimits) > 0 {
		var uls []string
		for _, k := range sortedKeys(svc.Ulimits) {
			ul := svc.Ulimits[k]
			if ul.Single != 0 {
				uls = append(uls, fmt.Sprintf("{name: %q, soft: %d}", k, ul.Single))
			} else {
				uls = append(uls, fmt.Sprintf("{name: %q, soft: %d, hard: %d}", k, ul.Soft, ul.Hard))
			}
		}
		u.addList("Container", "Ulimit", uls...)
	}
	u.addList("Container", "Label", labelList(m, svc.Labels)...)
	if svc.StopSignal != "" {
		u.add("Container", "StopSignal", strconv.Quote(svc.StopSignal))
	}
	if svc.StopGracePeriod != nil {
		u.add("Container", "StopTimeout", strconv.Itoa(int(time.Duration(*svc.StopGracePeriod).Round(time.Second)/time.Second)))
	}
	if svc.PullPolicy != "" {
		switch svc.PullPolicy {
		case "always", "never", "missing":
			u.add("Container", "Pull", strconv.Quote(svc.PullPolicy))
		case "if_not_present":
			u.add("Container", "Pull", `"missing"`)
		default:
			m.warnf("service %s: pull_policy %q not supported; dropped", name, svc.PullPolicy)
		}
	}
	// (The legacy log_driver/log_opt and net spellings are rejected by the
	// compose schema at load, so only the logging: block can reach here.)
	if svc.Logging != nil {
		if svc.Logging.Driver != "" {
			u.add("Container", "LogDriver", strconv.Quote(svc.Logging.Driver))
		}
		if len(svc.Logging.Options) > 0 {
			var lo []string
			for _, k := range sortedKeys(svc.Logging.Options) {
				lo = append(lo, strconv.Quote(k+"="+svc.Logging.Options[k]))
			}
			u.addList("Container", "LogOpt", lo...)
		}
	}
	// Restart policy is systemd's, not podman's.
	switch svc.Restart {
	case "", "no", "none":
	case "always":
		u.add("Service", "Restart", `"always"`)
	case "unless-stopped":
		// on-failure keeps the intent: a stopped container stays stopped.
		// (Restart=always would resurrect it after a podman stop, whose clean
		// exit looks restartable to systemd.) The one divergence: a container
		// that exits 0 on its own is not restarted.
		u.add("Service", "Restart", `"on-failure"`)
		m.warnf("service %s: restart unless-stopped mapped to on-failure (a clean self-exit stays stopped)", name)
	default:
		if strings.HasPrefix(svc.Restart, "on-failure") {
			u.add("Service", "Restart", `"on-failure"`)
			if _, retries, ok := strings.Cut(svc.Restart, ":"); ok {
				u.add("Service", "StartLimitBurst", retries)
			}
		} else {
			m.warnf("service %s: restart %q not recognized; dropped", name, svc.Restart)
		}
	}
}

func mapServiceHealthcheck(m *model, u *unitDef, name string, svc types.ServiceConfig) {
	hc := svc.HealthCheck
	if hc == nil {
		return
	}
	if hc.Disable {
		u.add("Container", "HealthCmd", `"none"`)
		return
	}
	if len(hc.Test) > 0 {
		switch hc.Test[0] {
		case "NONE":
			u.add("Container", "HealthCmd", `"none"`)
			return
		case "CMD-SHELL":
			// The remainder is one shell command string: pass through verbatim.
			u.add("Container", "HealthCmd", m.env.rewrite(strings.Join(hc.Test[1:], " ")))
		case "CMD":
			u.add("Container", "HealthCmd", m.env.rewrite(shellJoin(hc.Test[1:])))
		default:
			// The string form is shell syntax already.
			u.add("Container", "HealthCmd", m.env.rewrite(strings.Join(hc.Test, " ")))
		}
	}
	if hc.Interval != nil {
		u.add("Container", "HealthInterval", strconv.Quote(time.Duration(*hc.Interval).String()))
	}
	if hc.Timeout != nil {
		u.add("Container", "HealthTimeout", strconv.Quote(time.Duration(*hc.Timeout).String()))
	}
	if hc.Retries != nil {
		u.add("Container", "HealthRetries", strconv.FormatUint(*hc.Retries, 10))
	}
	if hc.StartPeriod != nil {
		u.add("Container", "HealthStartPeriod", strconv.Quote(time.Duration(*hc.StartPeriod).String()))
	}
	if hc.StartInterval != nil {
		m.warnf("service %s: healthcheck start_interval has no quadlet equivalent; dropped", name)
	}

	// A healthcheck without the notify wiring stays podman-only: quadlet's
	// default is conmon sending READY=1 at container start, so systemctl
	// start would return before the service is healthy and After= ordering
	// would wait for the wrong event. Notify=healthy postpones READY until
	// the healthcheck passes; NotifyAccess=all is required because the
	// healthy notification comes from the healthcheck process, not the main
	// PID (Type=notify is quadlet's default, set explicitly for clarity).
	u.add("Container", "Notify", `"healthy"`)
	u.add("Service", "Type", `"notify"`)
	u.add("Service", "NotifyAccess", `"all"`)

	// Worst-case time to healthy can exceed systemd's default 90s start
	// timeout (compose defaults alone are ~150s: 4x30s interval + 30s
	// timeout); give the start job room when the math demands it.
	const specDefault = 30 * time.Second
	interval, timeout := specDefault, specDefault
	if hc.Interval != nil {
		interval = time.Duration(*hc.Interval)
	}
	if hc.Timeout != nil {
		timeout = time.Duration(*hc.Timeout)
	}
	retries := uint64(3)
	if hc.Retries != nil {
		retries = *hc.Retries
	}
	var startPeriod time.Duration
	if hc.StartPeriod != nil {
		startPeriod = time.Duration(*hc.StartPeriod)
	}
	worst := startPeriod + time.Duration(retries+1)*interval + timeout
	if worst > 90*time.Second {
		// #TimeSpan is a string pattern.
		u.add("Service", "TimeoutStartSec", strconv.Quote(strconv.Itoa(int(worst/time.Second))+"s"))
	}
}

func mapServiceSecrets(m *model, p *types.Project, u *unitDef, name string, svc types.ServiceConfig) {
	var entries []string
	for _, s := range svc.Secrets {
		ref := sel("secrets", m.secretKeys.key(s.Source))
		var opts []string
		if s.Target != "" && s.Target != s.Source && s.Target != "/run/secrets/"+s.Source {
			opts = append(opts, "target: "+strconv.Quote(s.Target))
		}
		if s.UID != "" {
			opts = append(opts, "uid: "+s.UID)
		}
		if s.GID != "" {
			opts = append(opts, "gid: "+s.GID)
		}
		if s.Mode != nil {
			opts = append(opts, fmt.Sprintf("mode: %q", fmt.Sprintf("%04o", *s.Mode)))
		}
		if len(opts) > 0 {
			ref += " & {" + strings.Join(opts, ", ") + "}"
		}
		entries = append(entries, ref)
		if _, ok := p.Secrets[s.Source]; !ok {
			m.warnf("service %s: secret %q not declared top-level", name, s.Source)
		}
	}
	if len(entries) > 0 {
		u.addList("Container", "Secret", entries...)
	}
}

func mapServiceResources(m *model, u *unitDef, name string, svc types.ServiceConfig) {
	// Limits land in the systemd [Service] block: systemd owns the service
	// cgroup, so MemoryMax/CPUQuota/TasksMax constrain the whole unit.
	memory := int64(svc.MemLimit)
	cpus := float64(svc.CPUS)
	pids := svc.PidsLimit
	memoryLow := int64(svc.MemReservation)
	if svc.Deploy != nil {
		res := svc.Deploy.Resources
		if res.Limits != nil {
			if res.Limits.MemoryBytes > 0 {
				memory = int64(res.Limits.MemoryBytes)
			}
			if v := float64(res.Limits.NanoCPUs); v > 0 {
				cpus = v
			}
			if res.Limits.Pids > 0 {
				pids = res.Limits.Pids
			}
		}
		if res.Reservations != nil {
			if res.Reservations.MemoryBytes > 0 {
				memoryLow = int64(res.Reservations.MemoryBytes)
			}
			if float64(res.Reservations.NanoCPUs) > 0 {
				m.warnf("service %s: deploy cpu reservations have no absolute systemd equivalent (CPUWeight is proportional); dropped", name)
			}
			if len(res.Reservations.Devices) > 0 {
				m.warnf("service %s: deploy device reservations not supported; dropped", name)
			}
		}
		for _, skipped := range []struct {
			set  bool
			what string
		}{
			{svc.Deploy.Replicas != nil && *svc.Deploy.Replicas != 1, "replicas"},
			{svc.Deploy.UpdateConfig != nil, "update_config"},
			{svc.Deploy.RollbackConfig != nil, "rollback_config"},
			{svc.Deploy.RestartPolicy != nil, "restart_policy (use restart:)"},
			{svc.Deploy.Placement.Constraints != nil || svc.Deploy.Placement.Preferences != nil, "placement"},
			{svc.Deploy.Mode != "" && svc.Deploy.Mode != "replicated", "mode"},
			{len(svc.Deploy.Labels) > 0, "labels"},
			{svc.Deploy.EndpointMode != "", "endpoint_mode"},
		} {
			if skipped.set {
				m.warnf("service %s: deploy.%s is swarm-only; dropped", name, skipped.what)
			}
		}
	}
	// systemd's #Bytes is a string pattern (digits, suffixed, %, infinity).
	if memory > 0 {
		u.add("Service", "MemoryMax", strconv.Quote(strconv.FormatInt(memory, 10)))
	}
	// compose memswap_limit is memory+swap combined; systemd MemorySwapMax is
	// swap alone, so migrate the difference. -1 means unlimited swap.
	if ms := int64(svc.MemSwapLimit); ms != 0 {
		switch {
		case ms < 0:
			u.add("Service", "MemorySwapMax", `"infinity"`)
		case memory > 0 && ms >= memory:
			u.add("Service", "MemorySwapMax", strconv.Quote(strconv.FormatInt(ms-memory, 10)))
		default:
			m.warnf("service %s: memswap_limit %d without a covering memory limit cannot be converted (systemd MemorySwapMax is swap-only)", name, ms)
			u.dropComment("memswap_limit", ms)
		}
	}
	if memoryLow > 0 {
		u.add("Service", "MemoryLow", strconv.Quote(strconv.FormatInt(memoryLow, 10)))
	}
	if cpus > 0 {
		u.add("Service", "CPUQuota", strconv.Quote(strconv.FormatFloat(cpus*100, 'f', -1, 64)+"%"))
	}
	if pids > 0 {
		// #TasksLimit is a string pattern, like #Bytes.
		u.add("Service", "TasksMax", strconv.Quote(strconv.FormatInt(pids, 10)))
	}
}

func mapDependsOn(m *model, r refs, u *unitDef, name string, svc types.ServiceConfig, notifyHealthy map[string]bool) {
	if len(svc.DependsOn) == 0 {
		return
	}
	var after, requires []string
	for _, dep := range sortedKeys(svc.DependsOn) {
		cfg := svc.DependsOn[dep]
		ref := r.containerService(dep)
		switch cfg.Condition {
		case "", types.ServiceConditionStarted:
			after = append(after, ref)
			requires = append(requires, ref)
		case types.ServiceConditionHealthy:
			after = append(after, ref)
			requires = append(requires, ref)
			notifyHealthy[dep] = true
		case types.ServiceConditionCompletedSuccessfully:
			after = append(after, ref)
			m.warnf("service %s: depends_on %s condition service_completed_successfully mapped to After= only; review (a oneshot dependency may need Type=oneshot)", name, dep)
		default:
			m.warnf("service %s: depends_on %s condition %q not supported; mapped to After+Requires", name, dep, cfg.Condition)
			after = append(after, ref)
			requires = append(requires, ref)
		}
		if !cfg.Required {
			m.warnf("service %s: depends_on %s required=false has no direct mapping; emitted as a hard dependency", name, dep)
		}
	}
	u.addList("Unit", "After", after...)
	u.addList("Unit", "Requires", requires...)
}

// warnUnsupported reports service fields that are set but have no mapping:
// one line in the conversion report, and a comment in the emitted unit body
// carrying the original compose value.
func warnUnsupported(m *model, u *unitDef, name string, svc types.ServiceConfig) {
	for _, s := range []struct {
		set   bool
		field string
		val   any
		note  string
	}{
		{len(svc.Links) > 0, "links", svc.Links, "legacy; use networks + service DNS"},
		{len(svc.ExternalLinks) > 0, "external_links", svc.ExternalLinks, ""},
		{svc.Pid != "", "pid", svc.Pid, ""},
		{svc.Ipc != "", "ipc", svc.Ipc, ""},
		{svc.Uts != "", "uts", svc.Uts, ""},
		{svc.CgroupParent != "", "cgroup_parent", svc.CgroupParent, "systemd owns the cgroup"},
		{svc.Runtime != "", "runtime", svc.Runtime, ""},
		{svc.Platform != "", "platform", svc.Platform, ""},
		{len(svc.Gpus) > 0, "gpus", len(svc.Gpus), "use AddDevice with a CDI name"},
		{len(svc.Configs) > 0, "configs", len(svc.Configs), "mount the file explicitly or use a secret"},
		{svc.Develop != nil, "develop/watch", "set", ""},
		{len(svc.Annotations) > 0, "annotations", svc.Annotations, ""},
		{svc.OomScoreAdj != 0, "oom_score_adj", svc.OomScoreAdj, ""},
		{svc.MemSwappiness != 0, "mem_swappiness", svc.MemSwappiness, ""},
		{svc.CPUShares != 0, "cpu_shares", svc.CPUShares, "proportional; set CPUWeight in Service if needed"},
		{svc.CPUQuota != 0 || svc.CPUPeriod != 0, "cpu_quota/cpu_period", fmt.Sprintf("%d/%d", svc.CPUQuota, svc.CPUPeriod), "use cpus or deploy.resources.limits.cpus"},
		{svc.CPUSet != "", "cpuset", svc.CPUSet, ""},
		{svc.CPUCount != 0 || svc.CPUPercent != 0, "cpu_count/cpu_percent", fmt.Sprintf("%d/%g", svc.CPUCount, svc.CPUPercent), "Windows-only"},
		{svc.CPURTPeriod != 0 || svc.CPURTRuntime != 0, "cpu_rt_period/cpu_rt_runtime", fmt.Sprintf("%d/%d", svc.CPURTPeriod, svc.CPURTRuntime), ""},
		{svc.BlkioConfig != nil, "blkio_config", "set", "set IO* keys in Service if needed"},
		{len(svc.DeviceCgroupRules) > 0, "device_cgroup_rules", svc.DeviceCgroupRules, ""},
		{len(svc.StorageOpt) > 0, "storage_opt", svc.StorageOpt, ""},
		{svc.Isolation != "", "isolation", svc.Isolation, ""},
		{svc.Attach != nil && !*svc.Attach, "attach", false, "journald collects quadlet logs regardless"},
		{svc.Cgroup != "", "cgroup", svc.Cgroup, "namespace mode"},
		{svc.CredentialSpec != nil, "credential_spec", "set", "Windows-only"},
		{svc.DomainName != "", "domainname", svc.DomainName, ""},
		{len(svc.LabelFiles) > 0, "label_file", svc.LabelFiles, ""},
		{len(svc.Models) > 0, "models", len(svc.Models), ""},
		{svc.Provider != nil, "provider", "set", ""},
		{svc.UseAPISocket, "use_api_socket", true, ""},
		{svc.OomKillDisable, "oom_kill_disable", true, ""},
		{len(svc.PreStart) > 0 || len(svc.PostStart) > 0 || len(svc.PreStop) > 0, "pre_start/post_start/pre_stop", fmt.Sprintf("%d hook(s)", len(svc.PreStart)+len(svc.PostStart)+len(svc.PreStop)), "they run inside the container; systemd Exec* hooks run on the host"},
		{svc.Scale != nil && *svc.Scale != 1, "scale", derefOr(svc.Scale, 0), "systemd units are single-instance; use a template unit"},
		{svc.StdinOpen, "stdin_open", true, "services have no interactive stdin under systemd"},
		{svc.Tty, "tty", true, ""},
		{svc.VolumeDriver != "", "volume_driver", svc.VolumeDriver, "set driver on the named volume"},
		{len(svc.VolumesFrom) > 0, "volumes_from", svc.VolumesFrom, "mount the named volumes explicitly"},
	} {
		if !s.set {
			continue
		}
		what := s.field
		if s.note != "" {
			what += " (" + s.note + ")"
		}
		m.warnf("service %s: %s not supported; dropped", name, what)
		u.dropComment(s.field, s.val)
	}
}

func derefOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// --- small emission helpers ---

func listExpr(items []string) string {
	return "[" + strings.Join(items, ", ") + "]"
}

func quoteAll(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = strconv.Quote(s)
	}
	return out
}

func labelList(m *model, labels types.Labels) []string {
	var out []string
	for _, k := range sortedKeys(labels) {
		out = append(out, m.env.rewrite(k+"="+labels[k]))
	}
	return out
}

// jsonArray renders an entrypoint list in podman's JSON-array string form.
func jsonArray(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = strconv.Quote(a)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// shellJoin renders a command list as one command line, quoting arguments
// that need it (systemd-style double quotes).
func shellJoin(args []string) string {
	var out []string
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"'\\") {
			out = append(out, strconv.Quote(a))
		} else {
			out = append(out, a)
		}
	}
	return strings.Join(out, " ")
}
