package eval_test

import (
	"strings"
	"testing"
)

func quadletWith(container string) string {
	return "package naming\nimport q \"github.com/lugoues/creidhne@v0\"\n" +
		"app: q.#Quadlet & {name: \"app\", units: #container: " + container + "}\n"
}

// TestValidatorsAcceptPodmanValues locks down validators that were stricter than
// podman: every value here is accepted by podman/quadlet (IPv6 ports, bare /
// numeric / realtime signals, fractional durations, 3-digit octal secret mode)
// and must validate.
func TestValidatorsAcceptPodmanValues(t *testing.T) {
	accept := map[string]string{
		"ipv6 publish port":   `Container: {Image: "img", PublishPort: ["[::1]:80:90"]}`,
		"ipv6 range + proto":  `Container: {Image: "img", PublishPort: ["[2001:db8::1]:1234:1234/udp"]}`,
		"bare signal":         `Container: {Image: "img", StopSignal: "TERM"}`,
		"numeric signal":      `Container: {Image: "img", StopSignal: "9"}`,
		"realtime signal":     `Container: {Image: "img", StopSignal: "SIGRTMIN+3"}`,
		"fractional second":   `Container: {Image: "img", HealthInterval: "0.5s"}`,
		"fractional compound": `Container: {Image: "img", HealthInterval: "1.5h30m"}`,
		"3-digit octal mode":  `Container: {Image: "img", Secret: [{name: "s", type: "mount", mode: "400"}]}`,
		"ipv4-mapped ipv6 host port": `Container: {Image: "img", PublishPort: ["[::ffff:192.0.2.1]:8080:80"]}`,
		"instance service name":      `Container: {Image: "img", ServiceName: "worker@blue"}`,
	}
	for desc, cu := range accept {
		t.Run(desc, func(t *testing.T) {
			if err := loadSourceErr(t, quadletWith(cu)); err != nil {
				t.Errorf("want accepted, got: %v", err)
			}
		})
	}
}

// TestValidatorsStillRejectInvalid proves the relaxations did not over-loosen:
// non-octal modes, garbage ports, unitless durations, and (deliberately)
// negative durations stay rejected, since a negative interval/timeout is a
// config error for every field that uses #GoDuration.
func TestValidatorsStillRejectInvalid(t *testing.T) {
	reject := map[string]string{
		"non-octal mode":    `Container: {Image: "img", Secret: [{name: "s", type: "mount", mode: "999"}]}`,
		"garbage port":      `Container: {Image: "img", PublishPort: ["nope"]}`,
		"negative duration": `Container: {Image: "img", HealthInterval: "-5s"}`,
		"unitless duration": `Container: {Image: "img", HealthInterval: "5"}`,
	}
	for desc, cu := range reject {
		t.Run(desc, func(t *testing.T) {
			if err := loadSourceErr(t, quadletWith(cu)); err == nil {
				t.Errorf("want rejected, got accepted")
			}
		})
	}
}

// TestSchemaRejectsVacuousUnits locks out shapes that validate but render a
// unit podman cannot run (or that silently defeat a required field): an empty
// Rootfs, a Build with neither context nor Containerfile, a Yaml list whose
// nesting hides emptiness, and ServiceName values quadlet would mangle.
// (An empty build Context is handled by the renderer instead — see
// render.TestEmptyContextTreatedAsAbsent — because an incomplete-class
// validator like struct.MinFields would make the manifest comprehension's
// `if u != _|_` guard silently drop the unit rather than fail.)
func TestSchemaRejectsVacuousUnits(t *testing.T) {
	reject := map[string]string{
		"empty rootfs":              `#container: Container: {Rootfs: ""}`,
		"build with nothing":        `#build: Build: {}`,
		"kube nested empty yaml":    `#kube: Kube: Yaml: [[]]`,
		"kube empty yaml path":      `#kube: Kube: Yaml: [""]`,
		"empty service name":        `#container: Container: {Image: "img", ServiceName: ""}`,
		"service-suffixed override": `#container: Container: {Image: "img", ServiceName: "web.service"}`,
		// 248 chars: name + ".service" would reach systemd's UNIT_NAME_MAX.
		"overlong service name": `#container: Container: {Image: "img", ServiceName: "` + strings.Repeat("x", 248) + `"}`,
	}
	for desc, cu := range reject {
		t.Run(desc, func(t *testing.T) {
			src := "package naming\nimport q \"github.com/lugoues/creidhne@v0\"\n" +
				"app: q.#Quadlet & {name: \"app\", units: " + cu + "}\n"
			if err := loadSourceErr(t, src); err == nil {
				t.Errorf("want rejected, got accepted")
			}
		})
	}
}
