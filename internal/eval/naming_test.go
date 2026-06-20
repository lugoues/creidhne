package eval_test

import (
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue/load"

	"github.com/lugoues/creidhne"
	"github.com/lugoues/creidhne/internal/eval"
)

// loadSource evaluates a single main.cue against the embedded schema overlay.
func loadSource(t *testing.T, src string) []eval.Quadlet {
	t.Helper()
	tmp := t.TempDir()
	overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(tmp, "cue.mod", "module.cue")] = load.FromString(
		"module: \"example.com/naming@v0\"\nlanguage: version: \"v0.16.0\"\n")
	overlay[filepath.Join(tmp, "main.cue")] = load.FromString(src)
	quads, err := eval.LoadAndValidate(tmp, overlay)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return quads
}

// TestUnitRefAndService locks down #ref (filename) and #service for every kind.
// All units share the stem "svc" so the per-kind #serviceSuffix differences are
// the only thing that varies, so a typo in any kind's suffix fails here.
func TestUnitRefAndService(t *testing.T) {
	quads := loadSource(t, `package naming
import q "github.com/lugoues/creidhne@v0"
svc: q.#Quadlet & {
	name: "svc"
	units: {
		#container: Container: {Image: "img"}
		#pod: {}
		#volume: {}
		#network: {}
		#kube: Kube: Yaml: ["app.yaml"]
		#build: {ContainerFile: "FROM scratch\n", Build: ImageTag: ["localhost/x:latest"]}
		#image: Image: {Image: "img"}
		#artifact: Artifact: {Artifact: "example.com/a:v1"}
	}
}
`)
	if len(quads) != 1 {
		t.Fatalf("want 1 quadlet, got %d", len(quads))
	}
	type rs struct{ ref, service string }
	got := map[string]rs{}
	for _, u := range quads[0].Units {
		got[u.Kind] = rs{u.Filename, u.Service}
	}
	want := map[string]rs{
		"container": {"svc.container", "svc.service"},
		"pod":       {"svc.pod", "svc-pod.service"},
		"volume":    {"svc.volume", "svc-volume.service"},
		"network":   {"svc.network", "svc-network.service"},
		"kube":      {"svc.kube", "svc.service"},
		"build":     {"svc.build", "svc-build.service"},
		"image":     {"svc.image", "svc-image.service"},
		"artifact":  {"svc.artifact", "svc-artifact.service"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d kinds, want %d: %+v", len(got), len(want), got)
	}
	for kind, w := range want {
		if got[kind] != w {
			t.Errorf("%s: ref=%q service=%q; want ref=%q service=%q",
				kind, got[kind].ref, got[kind].service, w.ref, w.service)
		}
	}
}

// TestStemPlural covers the plural stem ("<quadlet>-<name>", name defaulting to
// the key). Here key "gw_tmp" is the CUE-side handle and name "gw-tmp" sets the
// hyphenated stem, producing app-gw-tmp.volume.
func TestStemPlural(t *testing.T) {
	quads := loadSource(t, `package naming
import q "github.com/lugoues/creidhne@v0"
app: q.#Quadlet & {
	name: "app"
	units: {
		containers: web: Container: {Image: "img"}
		volumes: "gw_tmp": {name: "gw-tmp", Volume: {}}
	}
}
`)
	got := map[string]string{} // filename -> service
	for _, u := range quads[0].Units {
		got[u.Filename] = u.Service
	}
	if got["app-web.container"] != "app-web.service" {
		t.Errorf("plural stem: app-web.container -> service %q, want app-web.service", got["app-web.container"])
	}
	if svc, ok := got["app-gw-tmp.volume"]; !ok || svc != "app-gw-tmp-volume.service" {
		t.Errorf("quoted-key stem should yield app-gw-tmp.volume / app-gw-tmp-volume.service, got %+v", got)
	}
}

// TestExternalRefs covers #ExtUnit naming for native (non-quadlet) units: a
// service with a name override, a socket, and a well-known target, resolved
// through a container's Unit.After at eval time.
func TestExternalRefs(t *testing.T) {
	quads := loadSource(t, `package naming
import q "github.com/lugoues/creidhne@v0"
externals: q.#ExternalUnits & {
	services: db: {name: "database"}
	sockets: podman: _
	targets: "network-online": _
}
app: q.#Quadlet & {
	name: "app"
	units: #container: {
		Container: {Image: "img"}
		Unit: After: [
			externals.services.db.#ref,
			externals.sockets.podman.#ref,
			externals.targets["network-online"].#ref,
		]
	}
}
`)
	var after []any
	for _, u := range quads[0].Units {
		if u.Kind == "container" {
			unit, _ := u.Data["Unit"].(map[string]any)
			after, _ = unit["After"].([]any)
		}
	}
	got := make([]string, len(after))
	for i, a := range after {
		got[i], _ = a.(string)
	}
	want := []string{"database.service", "podman.socket", "network-online.target"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("external #refs = %v, want %v", got, want)
	}
}

// TestResolvedResourceNames covers the #<type>Name meta fields: the resolved
// podman resource name (explicit XxxName, else systemd-<stem>), for each of the
// four named kinds, including the absent-section default and a keyed unit.
func TestResolvedResourceNames(t *testing.T) {
	quads := loadSource(t, `package naming
import q "github.com/lugoues/creidhne@v0"

db: q.#Quadlet & {name: "db", units: #container: Container: {Image: "img"}}
web: q.#Quadlet & {name: "web", units: #container: Container: {Image: "img", ContainerName: "custom-web"}}
store: q.#Quadlet & {name: "store", units: #volume: {}}
storenamed: q.#Quadlet & {name: "sn", units: #volume: Volume: {VolumeName: "myvol"}}
net: q.#Quadlet & {name: "net", units: #network: {}}
group: q.#Quadlet & {name: "group", units: #pod: {}}
app: q.#Quadlet & {name: "app", units: containers: side: Container: {Image: "img"}}

consumer: q.#Quadlet & {
	name: "consumer"
	units: #container: {
		Container: {Image: "img"}
		Unit: After: [
			db.units.#container.#containerName,
			web.units.#container.#containerName,
			store.units.#volume.#volumeName,
			storenamed.units.#volume.#volumeName,
			net.units.#network.#networkName,
			group.units.#pod.#podName,
			app.units.containers.side.#containerName,
		]
	}
}
`)
	var after []any
	for _, q := range quads {
		if q.Name != "consumer" {
			continue
		}
		unit, _ := q.Units[0].Data["Unit"].(map[string]any)
		after, _ = unit["After"].([]any)
	}
	got := make([]string, len(after))
	for i, a := range after {
		got[i], _ = a.(string)
	}
	want := []string{
		"systemd-db",       // container, default
		"custom-web",       // container, explicit
		"systemd-store",    // volume, default (absent Volume section)
		"myvol",            // volume, explicit
		"systemd-net",      // network, default (absent Network section)
		"systemd-group",    // pod, default (absent Pod section)
		"systemd-app-side", // keyed container, default (stem app-side)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("resolved names:\n got: %v\nwant: %v", got, want)
	}
}
