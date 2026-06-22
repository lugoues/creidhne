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

	// The raw string form must produce byte-identical output.
	raw := containerData(t, selfQuadlet(`{
		volumes: data: {Volume: {}}
		#container: Container: {Image: "img", Volume: ["app-data.volume:/etc/x:U"]}
	}`), "volumeStrings")
	if len(raw) != 1 || raw[0] != got[0] {
		t.Fatalf("raw form = %v, want same as #self form %v", raw, got)
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

// TestVolumeSlotRejectsForeignSelf is the brand: a network's #self cannot be
// placed in a Volume= slot (its _kind conflicts and it has no mount target).
func TestVolumeSlotRejectsForeignSelf(t *testing.T) {
	err := loadSourceErr(t, selfQuadlet(`{
		networks: net: {Network: {}}
		#container: Container: {Image: "img", Volume: [units.networks.net.#self]}
	}`))
	if err == nil {
		t.Error("want rejection: a network #self placed in a Volume= slot")
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

	raw := containerData(t, selfQuadlet(`{
		networks: net: {Network: {}}
		#container: Container: {Image: "img", Network: ["app-net.network"]}
	}`), "networkStrings")
	if len(raw) != 1 || raw[0] != got[0] {
		t.Fatalf("raw form = %v, want same as #self form %v", raw, got)
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

// TestNetworkRawModesStillAccepted: raw modes pass through unchanged (additive).
func TestNetworkRawModesStillAccepted(t *testing.T) {
	got := containerData(t, selfQuadlet(`{
		#container: Container: {Image: "img", Network: ["host", "none", "container:other"]}
	}`), "networkStrings")
	want := []any{"host", "none", "container:other"}
	if len(got) != len(want) {
		t.Fatalf("networkStrings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("networkStrings[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
