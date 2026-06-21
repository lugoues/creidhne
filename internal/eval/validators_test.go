package eval_test

import "testing"

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
		"ipv6 publish port":  `Container: {Image: "img", PublishPort: ["[::1]:80:90"]}`,
		"ipv6 range + proto": `Container: {Image: "img", PublishPort: ["[2001:db8::1]:1234:1234/udp"]}`,
		"bare signal":        `Container: {Image: "img", StopSignal: "TERM"}`,
		"numeric signal":     `Container: {Image: "img", StopSignal: "9"}`,
		"realtime signal":    `Container: {Image: "img", StopSignal: "SIGRTMIN+3"}`,
		"fractional second":  `Container: {Image: "img", HealthInterval: "0.5s"}`,
		"fractional compound": `Container: {Image: "img", HealthInterval: "1.5h30m"}`,
		"3-digit octal mode": `Container: {Image: "img", Secret: [{name: "s", type: "mount", mode: "400"}]}`,
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
