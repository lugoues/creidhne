# Helper validation hook (`#checks`)

Status: spec. Sequencing item 5 of the reverse-proxy design.

## Problem

A helper mixed into a quadlet cannot demand that its config be filled. The
canonical case: `#Quadlet & #TraefikProxySpec` with `#exposes` never set is
inert. Definitions are types, so `port!`/`rule!` trigger nothing until a
rendered path references them; hidden gates are not concreteness-checked
(verified, including `Validate(cue.Hidden(true))`); lint reads rendered
data, and an unfilled mixin leaves no trace in it. More generally, helpers
have no way to declare cross-field invariants ("if A then B") that fail the
build with a real message.

## Requirements

1. Declarable from any package (extras, user projects), not just creidhne.
2. Enforced on both gates: `crei validate` and the render path
   (render/plan/apply), with identical outcomes.
3. Zero cost and zero output change for quadlets that register nothing.
4. Failure messages name the check and carry a helper-authored explanation,
   not just a bare `incomplete value` path.
5. Checks never leak into rendered units, crei.state, graph, or import.

## Design

Three pieces, mirroring proven mechanisms: declaration rides a definition
field (cross-package, like `#rendered`), registration is a map (structs
merge under unification; lists conflict), and enforcement rides manifest
promotion (the only channel evaluation reliably forces).

### Declaration: `#Quadlet.#checks`

```cue
#Check: {
    // Values that must be concrete to render (deep: structs recurse).
    require?: [...]
    // Assertion that must hold; anything but true is a conflict.
    assert?: true
    // Shown when the check fails.
    why?: string
}

#Quadlet: {
    ...
    // Helper-registered invariants, keyed "<helper>/<check>".
    #checks: [Name=string]: #Check
}
```

A helper registers from its own literal (lexical scoping permitting, per
the `units: _` lesson):

```cue
#TraefikProxySpec: #ReverseProxySpec & {
    ...
    #checks: "traefik-proxy/exposes": {
        require: [#exposes.port, #exposes.rule]
        why:     "mixing #TraefikProxySpec requires #exposes: {port, rule}"
    }
}
```

Key collisions between helpers unify; conflicting bodies fail loudly, which
is correct (two helpers claiming one name is a bug).

### Promotion: the `checks` field

`#Quadlet` grows a computed regular field, same status as `manifest`
(user writes into it conflict with the comprehension):

```cue
checks: [for n, c in #checks {
    name: n
    if c.why != _|_ {why: c.why}
    if c.require != _|_ {require: c.require}
    if c.assert != _|_ {assert: c.assert}
}]
```

Promotion is what makes enforcement possible: `crei validate`'s
whole-instance `Validate(cue.Concrete(true))` now walks the checks for
free, and the render path forces them explicitly (below). An unset
`require` reference surfaces as an incomplete value; a false `assert`
surfaces as a `false != true` conflict. `#checks` empty means `checks: []`,
nothing forced, no output change.

### Enforcement and message mapping: `eval.tryQuadlet`

After the existing per-unit data validation, tryQuadlet validates the
quadlet's `checks` value with `cue.Concrete(true)`. On failure it maps the
cue error path (`checks.<i>...`) back to the decoded entry's `name` and
`why` (siblings of the failing field usually still decode) and reports:

```
quadlet grafana: check "traefik-proxy/exposes" failed: mixing
#TraefikProxySpec requires #exposes: {port, rule}
  (checks.0.require.0: incomplete value int & >0 & <65536)
```

Go then drops the value: `checks` never enters `eval.Quadlet`, state,
graph, or the importer.

## Failure modes

| State | Result |
| --- | --- |
| `require` ref not concrete | error at validate and render, named + why |
| `assert` evaluates false | conflict error, named + why |
| `assert` incomplete (depends on unset field) | incomplete error, named + why |
| no `#checks` | `checks: []`, zero overhead |

## Rejected alternatives

- Walking hidden fields in Validate: tested, references into definitions
  stay type-land; hidden fields are also package-scoped, so extras could
  not register anyway.
- Bare unification assertions without promotion (`_ok: expr & true`):
  nothing forces them (same reason the inert case exists).
- CUE attributes (`@check(...)`): attributes are static strings; they
  cannot reference values, so they cannot express "port must be concrete".
- List-shaped registry (`#checks: [...#Check]`): lists do not merge; two
  mixins on one quadlet would conflict.

## Non-goals

- Whole-graph cardinality (pair networks, attachment counts): stays in
  `crei lint`, keyed on rendered markers.
- Warning severity: v1 checks are errors; advisory findings belong to lint.
- Per-unit check scoping: quadlet scope reaches every unit already.

## Implementation sketch

1. Spike the loop end to end first: `#checks`/`checks` in quadlet.cue, one
   require-check in a fixture, prove both gates fire and the empty case is
   byte-identical output.
2. `creidhne/types.cue`: `#Check`. `creidhne/quadlet.cue`: `#checks` +
   `checks` promotion (manifest-contract change: document alongside the
   record shape).
3. `internal/eval/eval.go`: force + map + drop in tryQuadlet.
4. Tests: eval unit tests (require unset, assert false, assert
   incomplete, empty), a golden fixture with a passing check proving no
   output change, extras follow-up: register the traefik check.

## Open questions

1. Name: `#checks`/`checks` vs `#validate`/`validations`.
2. Should `#Check` stay closed (typo safety) with additive versioning, or
   open for forward compatibility?
3. Should `crei lint` list registered checks as an informational view?
