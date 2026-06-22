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
	units: #container: Container: {
		Image: "img"
		// Observe the resolved resource names via PodmanArgs (an arbitrary-string
		// field); strict dep fields like After= would reject non-service names.
		PodmanArgs: [
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
		container, _ := q.Units[0].Data["Container"].(map[string]any)
		after, _ = container["PodmanArgs"].([]any)
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

// TestUnitDepsAcceptServiceRefs is the positive companion to
// TestDepFieldsRejectNonService: the strict [Unit] dep fields accept and render
// a managed unit's #service and an external native unit's #ref (this is the
// After= coverage TestResolvedResourceNames used to provide incidentally before
// strict deps forced it onto PodmanArgs).
func TestUnitDepsAcceptServiceRefs(t *testing.T) {
	quads := loadSource(t, `package naming
import q "github.com/lugoues/creidhne@v0"
ext: q.#ExternalUnits & {targets: "network-online": _, sockets: podman: _}
app: q.#Quadlet & {
	name: "app"
	units: {
		volumes: data: {Volume: {}}
		#container: {
			Container: {Image: "img"}
			Unit: After: [units.volumes.data.#service, ext.targets["network-online"].#ref, ext.sockets.podman.#ref]
		}
	}
}
`)
	var after []string
	for _, u := range quads[0].Units {
		if u.Kind == "container" {
			unit, _ := u.Data["Unit"].(map[string]any)
			raw, _ := unit["After"].([]any)
			for _, a := range raw {
				s, _ := a.(string)
				after = append(after, s)
			}
		}
	}
	want := []string{"app-data-volume.service", "network-online.target", "podman.socket"}
	if strings.Join(after, ",") != strings.Join(want, ",") {
		t.Fatalf("After = %v, want %v", after, want)
	}
}

// loadSourceErr is loadSource's negative twin: it returns the load error instead
// of failing the test, for asserting that invalid input is rejected.
func loadSourceErr(t *testing.T, src string) error {
	t.Helper()
	tmp := t.TempDir()
	overlay, err := eval.Overlay(tmp, creidhne.SchemaFS)
	if err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(tmp, "cue.mod", "module.cue")] = load.FromString(
		"module: \"example.com/naming@v0\"\nlanguage: version: \"v0.16.0\"\n")
	overlay[filepath.Join(tmp, "main.cue")] = load.FromString(src)
	_, err = eval.LoadAndValidate(tmp, overlay)
	return err
}

// TestRejectsTraversalAndEmptyName locks down the unit-identity guard: a quadlet
// or unit name that is empty or contains path separators / ".." must be rejected
// at validation, since it feeds the on-disk filename (see the reconcile/render
// IsLocal guards for the defense-in-depth layer).
func TestRejectsTraversalAndEmptyName(t *testing.T) {
	cases := map[string]string{
		"traversal quadlet name": `app: q.#Quadlet & {name: "../escape", units: #container: Container: {Image: "img"}}`,
		"slash quadlet name":     `app: q.#Quadlet & {name: "a/b", units: #container: Container: {Image: "img"}}`,
		"empty quadlet name":     `app: q.#Quadlet & {name: "", units: #container: Container: {Image: "img"}}`,
		"traversal plural key":   `app: q.#Quadlet & {name: "app", units: volumes: "../evil": {Volume: {}}}`,
		"traversal plural name":  `app: q.#Quadlet & {name: "app", units: volumes: data: {name: "../evil", Volume: {}}}`,
	}
	for desc, body := range cases {
		src := "package naming\nimport q \"github.com/lugoues/creidhne@v0\"\n" + body + "\n"
		if err := loadSourceErr(t, src); err == nil {
			t.Errorf("%s: LoadAndValidate accepted invalid name, want error", desc)
		}
	}
}

// TestIncompleteUnitErrorNamesUnit guards the incomplete-unit message: when the
// missing field is the name itself, the derived filename is also empty, so the
// error must fall back to another identifier instead of reading "unit  is ...".
func TestIncompleteUnitErrorNamesUnit(t *testing.T) {
	src := "package naming\nimport q \"github.com/lugoues/creidhne@v0\"\n" +
		"app: q.#Quadlet & {units: #container: Container: {Image: \"img\"}}\n" // no name
	err := loadSourceErr(t, src)
	if err == nil {
		t.Fatal("want error for missing name")
	}
	if strings.Contains(err.Error(), "unit  is") {
		t.Errorf("blank unit identifier in error: %q", err.Error())
	}
}
