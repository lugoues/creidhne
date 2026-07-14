package eval_test

import (
	"strings"
	"testing"
)

// The design-doc specimens, each asserting the translated line and that the
// raw detail survives for debugging (docs/design/error-translation.md).
func TestDiagTranslations(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "named constraint with humanized path",
			body: `{#container: Container: {Image: "docker.io/x", Environment: ["NOEQUALS"]}}`,
			want: []string{
				`app (app.container): Container.Environment[0]`,
				`"NOEQUALS" is not a "key=value" pair (#KeyValue)`,
				"out of bound", // raw detail appended
			},
		},
		{
			name: "closedness did-you-mean",
			body: `{#container: Container: {Image: "docker.io/x", Enviroment: ["A=1"]}}`,
			want: []string{
				`app (app.container): Container.Enviroment`,
				`did you mean "Environment"?`,
			},
		},
		{
			name: "service name constraint",
			body: `{#container: {Container: Image: "docker.io/x", Unit: After: ["db"]}}`,
			want: []string{
				`"db" is not a systemd unit name`,
				"#ServiceName",
			},
		},
		{
			name: "unresolved helper in a label",
			body: `{#container: Container: {Image: "docker.io/x", Label: [{key: "x"}]}}`,
			want: []string{
				"unit app.container is incomplete",
				"Label (flattened)[0]",
				"#KeyValue",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := loadSourceErr(t, selfQuadlet(tc.body))
			if err == nil {
				t.Fatal("specimen must fail")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("missing %q in:\n%v", want, err)
				}
			}
		})
	}
}

// TestDiagPassThroughNotDuplicated: when translation is faithful (nothing
// collapsed or rewritten, e.g. reference-not-found), the raw detail must
// not be appended as a duplicate of the findings.
func TestDiagPassThroughNotDuplicated(t *testing.T) {
	err := loadSourceErr(t, selfQuadlet(`{#container: Container: {Image: "docker.io/x", Environment: ["A=\(#nope)"]}}`))
	if err == nil {
		t.Fatal("undefined reference must fail")
	}
	if got := strings.Count(err.Error(), `reference "#nope" not found`); got != 1 {
		t.Fatalf("message should appear exactly once, got %d:\n%v", got, err)
	}
}

// TestDiagDispatchErrorsLocated: comprehension-internal errors (xStrings
// dispatch) have numeric-only or empty paths. The ones touching user files
// are located via the dispatch guard line in the embedded schema plus the
// user position; the probe-arm noise gets no finding, and nothing garbage
// ("0: [0]:", "(root):") appears above the raw detail.
func TestDiagDispatchErrorsLocated(t *testing.T) {
	// Route 1: with a second (closedness) error the build stage fails and the
	// dispatch errors surface pathless; they must be located by position.
	err := loadSourceErr(t, selfQuadlet(`{
		#container: Container: {
			Image: "docker.io/x"
			Volume: [{target: "/data", options: ["U"]}]
		}
		volumes: data: Unit: Descripttion: "typo"
	}`))
	if err == nil {
		t.Fatal("bad volume entry must fail")
	}
	findings := err.Error()
	if i := strings.Index(findings, "\nbuild "); i >= 0 {
		findings = findings[:i]
	}
	// Whether the dispatch errors surface at this stage depends on cue's
	// evaluation order; when they do, locateByPosition names them (tested
	// white-box in diag_internal_test.go). The invariants here: the real
	// finding survives and no garbage locations appear.
	if !strings.Contains(findings, `did you mean "Description"?`) {
		t.Fatalf("closedness finding lost:\n%v", err)
	}
	for _, banned := range []string{"(root):", "\n0", "0.0:", "{#rendered:_}"} {
		if strings.Contains(findings, banned) {
			t.Fatalf("garbage finding %q leaked above the raw detail:\n%v", banned, err)
		}
	}

	// Route 2: alone, the same mistake surfaces at manifest decode with a
	// real path and is humanized normally.
	err = loadSourceErr(t, selfQuadlet(`{#container: Container: {
		Image: "docker.io/x"
		Volume: [{target: "/data", options: ["U"]}]
	}}`))
	if err == nil || !strings.Contains(err.Error(), "Container.Volume[0]") {
		t.Fatalf("decode-route dispatch error not humanized:\n%v", err)
	}
}

// TestDiagEmbeddedBottomExtracted: when a helper's list bottoms out inside
// an interpolation, cue buries the reason as _|_(...) in a huge conflict
// message; the finding must carry the reason, not a stub.
func TestDiagEmbeddedBottomExtracted(t *testing.T) {
	err := loadSourceErr(t, `package naming

import (
	"list"

	q "github.com/lugoues/creidhne@v0"
)

#spec: {
	port!: int
	#label: list.Concat([["traefik.port=\(port)"], ["traefik.enable=true"]])
}

app: q.#Quadlet & {
	name: "app"
	units: #container: Container: {Image: "docker.io/x", Label: [#spec.#label]}
}
`)
	if err == nil {
		t.Fatal("missing required port must fail")
	}
	findings := err.Error()
	if i := strings.Index(findings, "\nbuild "); i >= 0 {
		findings = findings[:i]
	}
	if !strings.Contains(findings, "required field missing: port") {
		t.Fatalf("embedded bottom reason not extracted:\n%v", err)
	}
	if strings.Contains(findings, "values…") {
		t.Fatalf("contentless stub leaked:\n%v", err)
	}
}

// TestDiagUnitInsteadOfSelfCollapses: a unit placed where its reference
// belongs fans out into per-field closedness rejections from every
// disjunction arm; they must collapse into one finding that names the fix.
func TestDiagUnitInsteadOfSelfCollapses(t *testing.T) {
	err := loadSourceErr(t, selfQuadlet(`{
		networks: net: {}
		#container: Container: {
			Image: "docker.io/x"
			Network: ["bridge", units.networks.net]
		}
	}`))
	if err == nil {
		t.Fatal("unit-instead-of-#self must fail")
	}
	findings := err.Error()
	if i := strings.Index(findings, "\nbuild "); i >= 0 {
		findings = findings[:i]
	}
	for _, want := range []string{"Container.Network[1]", "matches no accepted form here", `".#self"`} {
		if !strings.Contains(findings, want) {
			t.Fatalf("collapsed finding missing %q:\n%v", want, err)
		}
	}
	for _, banned := range []string{"mode: field not allowed", "name: field not allowed", "source: field not allowed"} {
		if strings.Contains(findings, banned) {
			t.Fatalf("arm-probe rejection leaked as finding %q:\n%v", banned, err)
		}
	}
}

// TestDiagStructDumpStaysTrimmed: a translated finding must never carry
// cue's multi-KB resolved-struct dump.
func TestDiagStructDumpStaysTrimmed(t *testing.T) {
	err := loadSourceErr(t, selfQuadlet(`{#container: Container: ContainerName: "x"}`))
	if err == nil {
		t.Fatal("missing Image|Rootfs must fail")
	}
	if len(err.Error()) > 1200 {
		t.Fatalf("error should be concise, got %d bytes:\n%s", len(err.Error()), err)
	}
}
