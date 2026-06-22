package eval_test

import "testing"

func selfQuadlet(body string) string {
	return "package naming\nimport q \"github.com/lugoues/creidhne@v0\"\n" +
		"app: q.#Quadlet & {name: \"app\", units: " + body + "}\n"
}

func containerData(t *testing.T, src, field string) []any {
	t.Helper()
	quads := loadSource(t, src)
	for _, u := range quads[0].Units {
		if u.Kind == "container" {
			out, _ := u.Data[field].([]any)
			return out
		}
	}
	t.Fatalf("no container unit / field %q in %s", field, src)
	return nil
}

// TestVolumeSelfFlattensToMountString: a decorated volume #self flattens to the
// same "source:target:options" string the raw form produces. This is the
// ergonomic win (typed reference) with identical output.
func TestVolumeSelfFlattensToMountString(t *testing.T) {
	got := containerData(t, selfQuadlet(`{
		volumes: data: {Volume: {}}
		#container: Container: {Image: "img", Volume: [units.volumes.data.#self & {target: "/etc/x", options: "U"}]}
	}`), "volumeStrings")
	if len(got) != 1 || got[0] != "app-data.volume:/etc/x:U" {
		t.Fatalf("volumeStrings = %v, want [app-data.volume:/etc/x:U]", got)
	}
}

// TestVolumeAcceptsHostMount: host binds and anonymous mounts stay raw strings.
func TestVolumeAcceptsHostMount(t *testing.T) {
	got := containerData(t, selfQuadlet(`{
		#container: Container: {Image: "img", Volume: ["/run/x.sock:/run/x.sock:ro", "/data"]}
	}`), "volumeStrings")
	want := []any{"/run/x.sock:/run/x.sock:ro", "/data"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("volumeStrings = %v, want %v", got, want)
	}
}

// TestVolumeSelfBareRefNoOptions covers a target without options.
func TestVolumeSelfBareRefNoOptions(t *testing.T) {
	got := containerData(t, selfQuadlet(`{
		volumes: data: {Volume: {}}
		#container: Container: {Image: "img", Volume: [units.volumes.data.#self & {target: "/data"}]}
	}`), "volumeStrings")
	if len(got) != 1 || got[0] != "app-data.volume:/data" {
		t.Fatalf("volumeStrings = %v, want [app-data.volume:/data]", got)
	}
}

// TestVolumeSlotRejectsBareSelfWithoutTarget: a Volume= ref must specify where it
// mounts; an undecorated volume #self (no target) is incomplete in the slot.
func TestVolumeSlotRejectsBareSelfWithoutTarget(t *testing.T) {
	err := loadSourceErr(t, selfQuadlet(`{
		volumes: data: {Volume: {}}
		#container: Container: {Image: "img", Volume: [units.volumes.data.#self]}
	}`))
	if err == nil {
		t.Error("want rejection: a volume #self in Volume= without a mount target")
	}
}

// TestNetworkSelfFlattens: a network #self flattens to its ref, identical to the
// raw string form; a container #self flattens to its .container ref (netns reuse).
func TestNetworkSelfFlattens(t *testing.T) {
	got := containerData(t, selfQuadlet(`{
		networks: net: {Network: {}}
		#container: Container: {Image: "img", Network: [units.networks.net.#self]}
	}`), "networkStrings")
	if len(got) != 1 || got[0] != "app-net.network" {
		t.Fatalf("networkStrings = %v, want [app-net.network]", got)
	}

	// A container #self in Network= is the netns-reuse form (name.container).
	cself := containerData(t, selfQuadlet(`{
		containers: one: Container: {Image: "img"}
		#container: Container: {Image: "img", Network: [units.containers.one.#self]}
	}`), "networkStrings")
	if len(cself) != 1 || cself[0] != "app-one.container" {
		t.Fatalf("container #self in Network = %v, want [app-one.container]", cself)
	}
}

// TestNetworkSlotRejectsVolumeSelf: the brand holds in the other direction too.
func TestNetworkSlotRejectsVolumeSelf(t *testing.T) {
	err := loadSourceErr(t, selfQuadlet(`{
		volumes: data: {Volume: {}}
		#container: Container: {Image: "img", Network: [units.volumes.data.#self]}
	}`))
	if err == nil {
		t.Error("want rejection: a volume #self placed in a Network= slot")
	}
}

// TestNetworkRawModesAccepted: the nameless podman modes pass through.
func TestNetworkRawModesAccepted(t *testing.T) {
	got := containerData(t, selfQuadlet(`{
		#container: Container: {Image: "img", Network: ["host", "none", "bridge", "slirp4netns:port_handler=slirp4netns", "ns:/run/netns/x"]}
	}`), "networkStrings")
	want := []any{"host", "none", "bridge", "slirp4netns:port_handler=slirp4netns", "ns:/run/netns/x"}
	if len(got) != len(want) {
		t.Fatalf("networkStrings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("networkStrings[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestNetworkRejectsRawRefForms: strict refuses bare .network strings, raw
// container:NAME netns reuse, and bare custom network names. Use #self/externals.
func TestNetworkRejectsRawRefForms(t *testing.T) {
	for _, bad := range []string{`["app-net.network"]`, `["container:systemd-cache"]`, `["mycustomnet"]`} {
		err := loadSourceErr(t, selfQuadlet(`{
			#container: Container: {Image: "img", Network: `+bad+`}
		}`))
		if err == nil {
			t.Errorf("strict Network= must reject %s", bad)
		}
	}
}

func scalarField(t *testing.T, src, field string) string {
	t.Helper()
	quads := loadSource(t, src)
	for _, u := range quads[0].Units {
		if u.Kind == "container" {
			s, _ := u.Data[field].(string)
			return s
		}
	}
	t.Fatalf("no container/field %q", field)
	return ""
}

// TestPodSelfFlattens: a pod #self flattens to its .pod ref, identical to raw.
func TestPodSelfFlattens(t *testing.T) {
	got := scalarField(t, selfQuadlet(`{
		#pod: {}
		#container: Container: {Image: "img", Pod: units.#pod.#self}
	}`), "podString")
	if got != "app.pod" {
		t.Fatalf("podString = %q, want app.pod", got)
	}
}

// TestPodSlotRejectsForeignSelf: only a pod #self fits a Pod= slot.
func TestPodSlotRejectsForeignSelf(t *testing.T) {
	err := loadSourceErr(t, selfQuadlet(`{
		networks: net: {Network: {}}
		#container: Container: {Image: "img", Pod: units.networks.net.#self}
	}`))
	if err == nil {
		t.Error("want rejection: a network #self placed in a Pod= slot")
	}
}

// TestImageSelfFlattens: a build/image #self flattens to its .build/.image ref;
// a raw image name passes through.
func TestImageSelfFlattens(t *testing.T) {
	got := scalarField(t, selfQuadlet(`{
		#build: {ContainerFile: "FROM scratch\n", Build: ImageTag: ["localhost/x:latest"]}
		#container: Container: {Image: units.#build.#self}
	}`), "imageString")
	if got != "app.build" {
		t.Fatalf("imageString = %q, want app.build", got)
	}
	raw := scalarField(t, selfQuadlet(`{
		#container: Container: {Image: "docker.io/x:latest"}
	}`), "imageString")
	if raw != "docker.io/x:latest" {
		t.Fatalf("raw image = %q, want docker.io/x:latest", raw)
	}
}

// TestImageRejectsRawRefString: a .build/.image ref written as a raw string is
// rejected; managed builds/images must be referenced via #self.
func TestImageRejectsRawRefString(t *testing.T) {
	for _, bad := range []string{`"app.build"`, `"app.image"`} {
		err := loadSourceErr(t, selfQuadlet(`{
			#container: Container: {Image: `+bad+`}
		}`))
		if err == nil {
			t.Errorf("strict Image= must reject raw %s (use #self)", bad)
		}
	}
}

// TestMountRefFlattens: a #MountRef builds a type=volume,source=...,destination=...
// string from a volume #self; raw mount strings pass through.
func TestMountRefFlattens(t *testing.T) {
	got := containerData(t, selfQuadlet(`{
		volumes: data: {Volume: {}}
		#container: Container: {Image: "img", Mount: [q.#MountRef & {ref: units.volumes.data.#self, destination: "/data", options: ["ro"]}]}
	}`), "mountStrings")
	want := "type=volume,source=app-data.volume,destination=/data,ro"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("mountStrings = %v, want [%s]", got, want)
	}
	raw := containerData(t, selfQuadlet(`{
		#container: Container: {Image: "img", Mount: ["type=tmpfs,destination=/tmp"]}
	}`), "mountStrings")
	if len(raw) != 1 || raw[0] != "type=tmpfs,destination=/tmp" {
		t.Fatalf("raw mount = %v, want [type=tmpfs,destination=/tmp]", raw)
	}
}

// TestMountRefRejectsForeignSelf: #MountRef.ref accepts only a volume/image #self.
func TestMountRefRejectsForeignSelf(t *testing.T) {
	err := loadSourceErr(t, selfQuadlet(`{
		networks: net: {Network: {}}
		#container: Container: {Image: "img", Mount: [q.#MountRef & {ref: units.networks.net.#self, destination: "/x"}]}
	}`))
	if err == nil {
		t.Error("want rejection: a network #self in #MountRef.ref")
	}
}

// TestDepFieldsRejectNonService: [Unit] dep fields take only #ServiceName. A
// podman ref (.container string or a #self struct) or a typo'd bare word is
// rejected; reference a managed unit's #service or an external native #ref.
func TestDepFieldsRejectNonService(t *testing.T) {
	for _, bad := range []string{`["app.container"]`, `["typo"]`, `[units.#pod.#self]`} {
		err := loadSourceErr(t, selfQuadlet(`{
			#pod: {}
			#container: {Container: {Image: "img"}, Unit: After: `+bad+`}
		}`))
		if err == nil {
			t.Errorf("strict After= must reject %s", bad)
		}
	}
}
