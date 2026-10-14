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

// TestDiagEmbeddedBottomExtracted: a helper's list bottoming out inside an
// interpolation must yield a located finding, not a stub. Under cue 0.16
// the reason arrived as a _|_(...) dump inside a huge conflict message
// (extracted by bottomRe); cue 0.17 reports a clean "error in call to
// list.Concat" but drops the underlying reason from the message entirely,
// so the finding asserts location over cause.
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
	if !strings.Contains(findings, "error in call to list.Concat") ||
		!strings.Contains(findings, "Label (flattened)") ||
		!strings.Contains(findings, "main.cue:16:63") {
		t.Fatalf("embedded bottom not located:\n%v", err)
	}
	if strings.Contains(findings, "values…") {
		t.Fatalf("contentless stub leaked:\n%v", err)
	}
}

// TestDiagIncompleteSecretEntry: an incomplete #SecretName in a Secret list
// (the shape a mixin input takes when its required field is left unset) makes
// the secretStrings flattener collapse to "list.Concat: empty disjunction:
// conflicting values string and [...]". That noise must be translated to name
// the section and the likely cause, not shown verbatim.
func TestDiagIncompleteSecretEntry(t *testing.T) {
	err := loadSourceErr(t, `package naming

import q "github.com/lugoues/creidhne@v0"

app: q.#Quadlet & {
	name: "app"
	units: #container: Container: {
		Image: "docker.io/x"
		// An incomplete #SecretRef nested in a sub-list (the schema allows
		// [...[...#SecretEntry]]): the outer list-shape check passes, so the
		// missing required name bites only at the secretStrings flatten, whose
		// disjunction then collapses to the cryptic "conflicting values string
		// and [...]" the escalation reported.
		Secret: [[{type: "env", target: "TOKEN"}]]
	}
}
`)
	if err == nil {
		t.Fatal("an incomplete #SecretName must fail")
	}
	findings := err.Error()
	if i := strings.Index(findings, "\nbuild "); i >= 0 {
		findings = findings[:i]
	}
	if !strings.Contains(findings, "an entry in Secret is incomplete") {
		t.Fatalf("incomplete secret entry not named:\n%v", err)
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

// TestDiagSecretEntryHint: unifying a #SecretRegistry entry directly into a
// Secret slot is rejected (the registryEntry marker), and the finding points
// at the .#ref handle instead of the generic .#self hint.
func TestDiagSecretEntryHint(t *testing.T) {
	err := loadSourceErr(t, `package naming

import q "github.com/lugoues/creidhne@v0"

secrets: q.#SecretRegistry & {db: _}
app: q.#Quadlet & {
	name: "app"
	units: #container: Container: {
		Image:  "docker.io/x"
		Secret: [secrets.db & {type: "env", target: "T"}]
	}
}
`)
	if err == nil {
		t.Fatal("direct entry consumption must fail")
	}
	if !strings.Contains(err.Error(), ".#ref") {
		t.Fatalf("finding should point at the .#ref handle:\n%v", err)
	}
}

// An unset required input (`field!:`) must be named. cue reports only the
// fallout, which can be an unrelated conflict (a UserNS interpolation over
// the missing field surfaces as "Container.Image: field not allowed") or a
// bare "invalid interpolation", neither of which mentions the input.
func TestDiagMissingRequiredInput(t *testing.T) {
	hdr := "package naming\nimport q \"github.com/lugoues/creidhne@v0\"\n"
	cases := []struct {
		name     string
		src      string
		validate bool // through Validate instead of LoadAndValidate
		want     []string
		not      string
	}{
		{
			name: "build-time conflict",
			src: hdr + `app: q.#Quadlet & {name: "app", #gid!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#gid):1"]}}}`,
			want: []string{
				"app: required input #gid is not set (int & >0)",
				"    main.cue:3:1\n    main.cue:3:33",
			},
			not: "field not allowed", // fallout is dropped, raw detail too
		},
		{
			name: "incomplete unit",
			src: hdr + `app: q.#Quadlet & {name: "app", #port!: int, units: #container: Container: {` +
				`Image: "docker.io/x", Environment: ["P=\(#port)"]}}`,
			want: []string{"unit app.container is incomplete", "required input #port is not set (int)"},
		},
		{
			name: "nested under a definition",
			src: hdr + `app: q.#Quadlet & {name: "app", #images: {debian!: string}, units: #container: Container: {` +
				`Image: "docker.io/x", Environment: ["I=\(#images.debian)"]}}`,
			want: []string{"required input #images.debian is not set (string)"},
		},
		{
			name: "nested quadlet",
			src: hdr + `stacks: web: q.#Quadlet & {name: "web", #gid!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#gid):1"]}}}`,
			want: []string{"stacks.web: required input #gid is not set"},
		},
		// The typo breaks the same Container disjunction, so filling #gid
		// clears nothing and it is not blamed yet; the typo must still show.
		{
			name: "independent error is not masked",
			src: hdr + `app: q.#Quadlet & {name: "app", #gid!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", Enviroment: ["A=1"], UserNS: {mode: "auto", gidmapping: ["1000:\(#gid):1"]}}}`,
			want: []string{`did you mean "Environment"?`},
		},
		// A stand-in the input's own constraint rejects can't be judged; the
		// typo must not be swallowed as fallout of the bottomed probe.
		{
			name: "constraint rejects the stand-in",
			src: hdr + `app: q.#Quadlet & {name: "app", #target!: string & =~"^[^:]+:[0-9]+$", units: #container: Container: {` +
				`Image: "docker.io/x", Enviroment: ["A=1"], Environment: ["T=\(#target)"]}}`,
			want: []string{`did you mean "Environment"?`},
		},
		{
			name: "bytes input",
			src: hdr + `app: q.#Quadlet & {name: "app", #payload!: bytes, units: #container: Container: {` +
				`Image: "docker.io/x", Environment: ["P=\(#payload)"]}}`,
			want: []string{"required input #payload is not set (bytes)"},
		},
		// An ordinary field named manifest on an ancestor is not a quadlet;
		// ownership must reach the nested one beneath it.
		{
			name: "non-quadlet manifest on an ancestor",
			src: hdr + `stacks: {manifest: "metadata", web: q.#Quadlet & {name: "web", #gid!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#gid):1"]}}}}`,
			want: []string{"stacks.web: required input #gid is not set"},
		},
		// "stacks.web" and stacks.web are distinct owners; both must be
		// scanned. Only validate reports both at once (a build error carries
		// just the first failing value).
		{
			name:     "quoted and nested owners stay distinct",
			validate: true,
			src: hdr + `"stacks.web": q.#Quadlet & {name: "a", #pa!: int, units: #container: Container: {` +
				`Image: "docker.io/x", Environment: ["P=\(#pa)"]}}` + "\n" +
				`stacks: web: q.#Quadlet & {name: "b", #pb!: int, units: #container: Container: {` +
				`Image: "docker.io/x", Environment: ["P=\(#pb)"]}}`,
			want: []string{"required input #pa is not set", "required input #pb is not set"},
		},
		// Several missing inputs all surface in one run.
		{
			name: "two inputs breaking the same field",
			src: hdr + `app: q.#Quadlet & {name: "app", #gid!: int & >0, #uid!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#gid):1"], uidmapping: ["1000:\(#uid):1"]}}}`,
			want: []string{"required input #gid is not set", "required input #uid is not set"},
			not:  "field not allowed",
		},
		{
			name:     "input hidden behind another's build failure",
			validate: true,
			src: hdr + `app: q.#Quadlet & {name: "app", #gid!: int & >0, #port!: int, units: #container: Container: {` +
				`Image: "docker.io/x", Environment: ["P=\(#port)"], UserNS: {mode: "auto", gidmapping: ["1000:\(#gid):1"]}}}`,
			want: []string{"required input #gid is not set", "required input #port is not set"},
		},
		{
			name:     "every failing quadlet",
			validate: true,
			src: hdr + `a: q.#Quadlet & {name: "a", #ga!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#ga):1"]}}}` + "\n" +
				`b: q.#Quadlet & {name: "b", #gb!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#gb):1"]}}}`,
			want: []string{"a: required input #ga is not set", "b: required input #gb is not set"},
		},
		// A grouping value with an ordinary manifest list is not a quadlet;
		// the scan must still reach the quadlet beneath it.
		{
			name: "quadlet below a non-quadlet manifest list",
			src: hdr + `a: q.#Quadlet & {name: "a", #ga!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#ga):1"]}}}` + "\n" +
				`stacks: {manifest: [{note: "x"}], web: q.#Quadlet & {name: "web", #gb!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#gb):1"]}}}}`,
			want: []string{"a: required input #ga is not set", "stacks.web: required input #gb is not set"},
		},
		// Interchangeable inputs: filling either one alone satisfies the
		// field, so leave-one-out clears both; each must still be named.
		{
			name: "alternative inputs",
			src: hdr + `app: q.#Quadlet & {name: "app", #inputs: {a!: string, b!: string}, units: #container: Container: {` +
				`Image: #inputs.a | #inputs.b}}`,
			want: []string{"required input #inputs.a is not set", "required input #inputs.b is not set"},
		},
		// A render-path build error must not drag in another quadlet whose
		// unset input only matters to strict validation (it never reaches
		// the manifest, so render and apply succeed without it).
		{
			name: "strict-only input stays out of an ordinary load",
			src: hdr + `a: q.#Quadlet & {name: "a", #ga!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#ga):1"]}}}` + "\n" +
				`b: q.#Quadlet & {name: "b", #value!: string, note: "\(#value)", units: #container: Container: Image: "docker.io/x"}`,
			want: []string{"a: required input #ga is not set"},
			not:  "#value",
		},
		// The second quadlet isn't bottom, only its manifest is incomplete:
		// it fails render too, so it's named in the same run. Its strict-only
		// input (feeding a field outside the manifest) still isn't.
		{
			name: "incomplete manifest beside a build failure",
			src: hdr + `a: q.#Quadlet & {name: "a", #ga!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#ga):1"]}}}` + "\n" +
				`b: q.#Quadlet & {name: "b", #port!: int, #value!: string, note: "\(#value)", units: #container: Container: {` +
				`Image: "docker.io/x", Environment: ["P=\(#port)"]}}`,
			want: []string{"a: required input #ga is not set", "b: required input #port is not set"},
			not:  "#value",
		},
		// Render only validates each manifest record's data; an incomplete
		// extra field on a record must not be blamed just because another
		// quadlet failed.
		{
			name: "incomplete field beside manifest data",
			src: hdr + `a: q.#Quadlet & {name: "a", #ga!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#ga):1"]}}}` + "\n" +
				`b: q.#Quadlet & {name: "b", #value!: string, manifest: [{debug: #value}, ...], units: #container: Container: Image: "docker.io/x"}`,
			want: []string{"a: required input #ga is not set"},
			not:  "#value",
		},
		// renderErrors applies checkRecord, tryQuadlet's contract: data is
		// validated even when absent, and an identity field present but not
		// concrete fails too. Hand-built manifests isolate both.
		{
			name: "absent data counts, as render fails on it",
			src: hdr + `a: q.#Quadlet & {name: "a", #ga!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#ga):1"]}}}` + "\n" +
				`b: {#enabled!: bool, manifest: [{kind: "volume", stem: "b", filename: "b.volume", service: "b-volume.service", ` +
				`if #enabled {data: {Volume: {}}}}]}`,
			want: []string{"a: required input #ga is not set", "b: required input #enabled is not set"},
		},
		{
			name: "incomplete identity field",
			src: hdr + `a: q.#Quadlet & {name: "a", #ga!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#ga):1"]}}}` + "\n" +
				`b: {#svc!: string, manifest: [{kind: "volume", stem: "b", filename: "b.volume", service: #svc, data: {Volume: {}}}]}`,
			want: []string{"a: required input #ga is not set", "b: required input #svc is not set"},
		},
		// A record with an empty kind isn't a quadlet to tryQuadlet, so its
		// absent data isn't a render failure either.
		{
			name: "empty kind is not a quadlet",
			src: hdr + `a: q.#Quadlet & {name: "a", #ga!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#ga):1"]}}}` + "\n" +
				`b: {#enabled!: bool, manifest: [{kind: "", if #enabled {data: {}}}]}`,
			want: []string{"a: required input #ga is not set"},
			not:  "#enabled",
		},
		{
			name: "failure before an empty kind still counts",
			src: hdr + `a: q.#Quadlet & {name: "a", #ga!: int & >0, units: #container: Container: {` +
				`Image: "docker.io/x", UserNS: {mode: "auto", gidmapping: ["1000:\(#ga):1"]}}}` + "\n" +
				`b: {#enabled!: bool, manifest: [{kind: "volume", if #enabled {data: {Volume: {}}}}, {kind: ""}]}`,
			want: []string{"a: required input #ga is not set", "b: required input #enabled is not set"},
		},
		// Unset but inert: an unused required field, or one under an absent
		// optional definition, must not take the blame for an unrelated typo.
		{
			name: "unused required field",
			src: hdr + `app: q.#Quadlet & {name: "app", #unused: {foo!: string}, units: #container: Container: {` +
				`Image: "docker.io/x", Enviroment: ["A=1"]}}`,
			want: []string{`did you mean "Environment"?`},
			not:  "#unused",
		},
		{
			name: "absent optional definition",
			src: hdr + `app: q.#Quadlet & {name: "app", #opt?: {foo!: string}, units: #container: Container: {` +
				`Image: "docker.io/x", Enviroment: ["A=1"]}}`,
			want: []string{`did you mean "Environment"?`},
			not:  "#opt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			load := loadSourceErr
			if tc.validate {
				load = validateSourceErr
			}
			err := load(t, tc.src)
			if err == nil {
				t.Fatal("specimen must fail")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("missing %q in:\n%v", want, err)
				}
			}
			if tc.not != "" && strings.Contains(err.Error(), tc.not) {
				t.Fatalf("%q must not be a finding:\n%v", tc.not, err)
			}
		})
	}
}

// An identity field present but not concrete used to decode as "" and fail
// later in render as an unsafe output path; it now fails at load, naming the
// input. An absent one is fine (some kinds generate no service).
func TestRecordIdentityFields(t *testing.T) {
	hdr := "package naming\n"
	err := loadSourceErr(t, hdr+`b: {#fn!: string, manifest: [{kind: "volume", stem: "b", filename: #fn, service: "b-volume.service", data: {Volume: {}}}]}`)
	if err == nil || !strings.Contains(err.Error(), "required input #fn is not set") {
		t.Fatalf("incomplete filename: want the input named, got %v", err)
	}
	if err := loadSourceErr(t, hdr+`b: {manifest: [{kind: "volume", stem: "b", filename: "b.volume", data: {Volume: {}}}]}`); err != nil {
		t.Fatalf("absent service must load: %v", err)
	}
}
