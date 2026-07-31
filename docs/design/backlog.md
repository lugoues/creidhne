# Backlog

Queued work items, roughly by value. Each carries enough design context to
pick up cold; promote an item to its own design doc when execution starts.

## Diagnostics (TOP PRIORITY — errors are way too cryptic)

User escalation 2026-07-23 after the shelfmark incident: a missing
required mixin input (`#gluetun.auth_config`, a `#SecretName`) surfaced as
`secretStrings: error in call to list.Concat: 2 errors in empty
disjunction: conflicting values string and [...]` — an incomplete struct
inside a nested Secret list misroutes the flattener's guard probe and the
real cause (incomplete input) never appears.

- DONE: the flattened-field signature (`empty disjunction` /
  string-vs-list on `secretStrings`/`volumeStrings`/... groups) now maps
  to "an entry in <Section> is incomplete: a required input is unset",
  keeping the raw error below. `diagnose` decides over the whole error
  group (the informative arm is the bare list.Concat; the markers live in
  sibling arms, read via each arm's full text). See `isIncompleteFlatten`.
- Still open: name the *incomplete leaf* itself (print `Secret[0].name`,
  not just the section). BLOCKER found 2026-07-24: the failing entry
  evaluates to bottom (`_|_`) — the same incompleteness that triggers the
  error poisons the value — so `.List()`/`.Fields()` on it dead-end and a
  structural walk from `root` finds nothing (verified: the section value
  resolves via an optional-aware field scan, but its entries are `_|_`).
  Naming the leaf therefore can't read the evaluated value; it needs a
  different route: parse the user's source syntax for the entry, unify it
  against the section's entry definition (#SecretRef/#VolumeMount/...) in
  isolation, and `Validate(cue.Concrete(true))` that to name the missing
  required field. Schema-coupled and fragile; deferred as low-ROI over the
  section message, which already points at the cause.
- Existing sub-items from the earlier diagnostics entry:
  - `checkFailures` maps any error whose path traverses `#checks."<name>"`
    into a `check failed` line; map only genuine assert conflicts (see
    helper-validation-hook.md).
  - DONE: constraint-name table now derived from the embedded schema
    (`internal/eval/constraints.go`: parse the .cue source, collect every
    `=~"..."` under a definition → name); only the human hint stays hand-
    kept, keyed by the stable definition name, and a guard test asserts
    every hinted definition still declares a regex. `--raw-errors` bypasses
    translation entirely.

## Graph validation pass (lint infrastructure)

The other half of helper validation: `#checks` is quadlet-scoped and CUE
cannot enumerate sibling quadlets, so cross-quadlet invariants need a Go
pass over the full manifest set (which eval already has in memory).

- After `LoadAndValidate`, build the project graph: networks indexed by
  filename with labels; every container's `Network=` entries parsed back
  to `<stem>.network` refs; runs inside `validate` and `plan`.
- Rules are keyed by marker labels so they need no knowledge of the
  helpers that placed them (the `creidhne.pair=<name>` contract).
- First rule, pair cardinality: count attachers of each `creidhne.pair`
  network across all quadlets. More than two is an error (isolation
  contract broken); fewer than two is a warning (wiring incomplete,
  proxy not yet attached — legitimate mid-migration).
- Follow-on rules, one function each: duplicate `ContainerName` across
  quadlets (breaks hosts books), duplicate traefik router names across
  quadlets, raw-string `Network=` refs to units not in the project,
  orphan networks with zero attachers, `PublishPort` on containers
  attached only to internal networks (wants a `creidhne.internal` marker
  on `#InternalNetworkSpec`, not yet emitted).
- Caveat to document: this proves the project's declared graph, not
  runtime state; out-of-project attachers are invisible. Runtime
  comparison (podman's actual attachments vs manifest) is a separate
  `crei status` enhancement.

## Quadlet import

Onboard existing quadlet users: parse unit files into the importer's
section-shaped model (near-identity mapping; compose reuses the same emit
backend). Key design points from the assessment: prefix clustering is
forced by the stem grammar (`<quadlet>-<name>`); handle-ification is
mandatory for volume/network/pod refs (schema rejects raw strings there);
unknown keys surface as schema gaps by validating importer output
in-process before writing; a `--verify` mode semantically diffs input vs
rendered output (key multisets per section) since byte-diff shows
harmless normalization. Punt v1: template units, drop-ins, kube, external
`File=` Containerfiles. Land after the import-form-guard release.

## Status / operations

- `crei up`: the converge composite now that start/stop exist — apply,
  start what's down, offer restart --stale for what's outdated; the
  compose-shaped "make reality match the CUE" one-liner. start/stop are
  the primitives; up is sugar over apply + start + restart --stale and
  should stay that thin.

- `crei status`: when a unit failed with result `dependency`, chase
  `Requires=` edges to the failed dependency and show the root cause.
- `crei secrets rotate` (re-read sources, update changed secrets).
- `crei outdated`: revive the digest work from
  transactional-apply-and-auto-update.md.
- Volume migration: the acting half of "recreate required". A stale
  volume's change never applies (create --ignore no-ops) and podman has
  no volume rename, so applying means recreate-in-place:
  `crei volume migrate <unit>` — resolve attachers from the project
  graph (containers whose Volume= references it), refuse while any is
  running, stop them on request, `podman volume export` to a backup
  tarball (kept unless --no-backup), remove, recreate from the new
  config (run the volume unit), `podman volume import`, restart the
  attachers. Preserve ownership/SELinux via tar flags; verify the
  backup is non-empty before the destructive step. Status/diff --stale
  already identify candidates and what changes.
- Staleness tier 3 (tiers 1-2 shipped: history in crei.state, status
  stale annotations, diff/restart/logs --stale): podman-inspect drift
  comparison for live volumes/networks vs desired options, no history
  needed — answers "is the live object right" independent of when it
  was created.

## Schema / language

- Unit-scoped `#checks`: unit-level helpers cannot register invariants
  today (the hook is quadlet-scoped); surfaced twice building extras.
- Upstream cue evalv3 false-cycle reports, state as of 0.17.1:
  - `list.FlattenN` over lists referencing the instance under
    construction (traefik label check) still freezes; the lazy
    comprehension over identical data resolves.
  - Cross-member `#containerName` probing was fixed in 0.17.1 — the
    injected-ContainerName workaround in extras can be simplified once
    the deployed evaluator floor is >= 0.17.1.
  - NEW in 0.17.1: spurious "invalid interpolation" on the route-port
    pattern. Fully minimized 74-line single-file CLI repro + evidence
    in docs/design/cue-0171-interpolation-regression.md — ready to
    file against cue-lang/cue (unreported as of 2026-07-20).
- Upstream podman docs PR: `podman-network-create.1.md` documents only
  the bool form of `isolate`; `strict` (netavark >= 1.7, default in 2.0)
  is invisible outside netavark release notes.
