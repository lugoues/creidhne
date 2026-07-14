# Error translation layer

Status: spec / exploration.

## Problem

cue's raw errors are structurally accurate and humanly useless. A one-typo
project produces this (real output):

```
app.units.#container.Container: 3 errors in empty disjunction:
app.units.#container.Container.Environment.0: 2 errors in empty disjunction:
app.units.#container.Container.Environment.0: conflicting values "NOEQUALS" and [...#KeyValue] (mismatched types string and list):
    .../cue.mod/usr/github.com/lugoues/creidhne/container.cue:5:13
    ... five more schema-internal positions ...
app.units.#container.Container.Environment.0: invalid value "NOEQUALS" (out of bound =~"^[^=]+=.*$"):
    ...
```

The story is one line: `Environment[0] "NOEQUALS" is not a key=value pair`.
Everything else is disjunction bookkeeping, the loosened-list arm, schema
positions, and an unnamed regex. Session-collected specimens of the same
disease: the `#LabelValue` empty-disjunction explosion on a typo'd helper
field (10 arms deep), `labelStrings.3: incomplete value =~"^[^=]+=.*$"` for
an unresolved helper, and `manifest.0.data...` paths leaking the CUE-Go
contract into user-facing text.

## Confirmed API facts (specimen-tested)

- `errors.Errors(err)` flattens to individually addressable errors, each
  with `Path()` (structured, no parsing) and `Position()`/`InputPositions()`.
- Disjunction arm failures appear as separate same-path entries alongside
  an "N errors in empty disjunction" header entry.
- The `(T | [...T])` loosening means every scalar failure carries a
  guaranteed-noise "mismatched types X and list" arm.
- Precedent in-tree: `checkFailures` already translates one error class by
  path shape; `incompleteHint` already trims struct dumps.

## Architecture

A translator in `internal/eval` (diag.go): `diagnose(root cue.Value, err
error) []finding`, applied at the three gates (`buildInstance`,
`tryQuadlet`, `Validate`) before falling back to today's `cueError` output.
Matchers are conservative: anything unrecognized passes through verbatim,
and the raw (trimmed) detail is always appended, so `want_error` fixtures
and debuggability survive. The layer only affects crei commands; editor/LSP
output comes from cue directly and is out of scope.

Pipeline per error group (grouped by `Path()`):

1. **Collapse disjunctions**: drop "empty disjunction" headers; among
   same-path arms drop "mismatched types … and list" (the loosening arm)
   and prefer the most specific survivor (constraint violation > type
   mismatch > incomplete).
2. **Humanize the path**: resolve against the root value.
   `manifest.N.data.X` -> unit filename + field; `units.#container.
   Container.X` -> `<stem>.container: Container.X`; `labelStrings.N` and
   the other xStrings -> their source field (`Label[N]`), via a small
   comprehension<->field table.
3. **Name the constraint**: map the schema's load-bearing regexes/enums to
   their type names and a one-line expectation (#KeyValue "key=value",
   #ServiceName "must end in .service/.target/...", #PortMapping,
   #UnitName, #Ulimit, #UserNS). Hand-kept Go table of the top offenders;
   the table cites the schema so drift is a two-edit rule like templates.
4. **Filter positions**: keep positions in the user's project; keep at most
   one schema position (the constraint definition), dim or drop the rest.
5. **Suggest on closedness**: "field not allowed" + the parent's allowed
   field set -> nearest-name suggestion ("Enviroment: field not allowed;
   did you mean Environment?").

Target rendering for the specimen:

```
Error: app (app.container): Container.Environment[0]: "NOEQUALS" is not a
key=value pair (#KeyValue)
    main.cue:10:17
```

## Matcher catalog v1

| Class | Trigger | Translation |
| --- | --- | --- |
| Disjunction noise | "empty disjunction" header, list-arm mismatch | collapse to best arm |
| Named constraint | regex/enum from the table | value + type name + expectation |
| Closedness | "field not allowed" | location + did-you-mean |
| Unresolved helper | incomplete at an xStrings index | "Label[N] never resolved; a helper's #rendered is unset" |
| Contract paths | `manifest.N.data`, xStrings, `#container` | user-coordinate rewrite |

Deferred: structural-cycle explainer, broader enum coverage, colorized
rendering, translating `cue vet` (schema-side) output.

## Risks

- Matching partially couples to cue's message strings (paths are stable
  API; messages are not). Mitigation: conservative matchers, verbatim
  fallback, table-driven tests reproducing each specimen so a cue upgrade
  flags drift loudly.
- Heuristic arm-picking can guess wrong on genuinely ambiguous
  disjunctions; the appended raw detail keeps the full story available.

## Implementation sketch

1. Spike: reproduce the five specimens as table-driven fixtures; assert
   `errors.Errors` exposes what each matcher needs (paths, arm grouping).
2. `internal/eval/diag.go`: grouping + collapse + path humanization
   (matchers 1, 2, 4) — the universal wins, no message-string coupling
   beyond "empty disjunction"/"mismatched types".
3. Constraint table + closedness suggestions (matchers 3, 5).
4. Wire into the three gates; keep `checkFailures` as-is (it already owns
   the #checks class); goldens: new invalid fixtures asserting translated
   lines, existing want_error files untouched via appended raw detail.
