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
