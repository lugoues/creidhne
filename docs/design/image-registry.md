# Image registry & update management

Status: phases 1-2 shipped (registry, add/pin/outdated/update/lock,
semver range, lint config); CVE thresholds and the secrets registry
remain.

## Problem

Containers pin images. For reproducibility a pin must be a digest
(`@sha256:…`) — a tag (`:latest`, `:v3`) lets the registry change what
runs out from under the config. But digest pins go stale: a newer image
exists upstream and you have no signal to bump.

Podman's `AutoUpdate=registry` is the wrong answer for a declarative tool:
it mutates the *running* container out of band, so the CUE no longer
describes reality and `crei plan`/`diff` go blind (the `.container` file is
byte-identical; only the pulled digest moved, which crei doesn't track).
It moves the decision of *what runs* from the config to a registry+timer.
That is imperative drift bolted onto a declarative reconciler.

The declarative equivalent is **config write-back**: detect drift, bump the
pin *in the source*, then reconcile applies it — every image change a
reviewable, reproducible edit. crei owns the source, so crei is the right
home for it.

## The channel/digest pair

A digest alone loses the channel it came from, so crei can't check it for
updates; a tag alone isn't pinned. You need both — but as two fields, not
one combined string, so intent and lock stay separate:

    gluetun: {image: "docker.io/qmcgaw/gluetun:v3", digest: "sha256:ad6b…"}

- `image` is the tracked channel you hand-edit (registry/repo:tag, no
  digest). `crei image pin` never rewrites it.
- `digest` is the pin, written by `crei image pin`.
- `#ref` is computed (`image@digest`, or bare `image` when unpinned) — the
  handle a container consumes. Podman pulls by the digest; the tag rides
  along for crei to resolve.

Classification, from the two fields:

- tag + digest → **managed**: pinned and updatable.
- tag, no digest → **unpinned**: not reproducible (lint).
- no trackable tag (digest only) → **unmanaged**: pinned but crei can't
  offer updates (lint; give `image` a tag to manage it).

## The `registries/` package

crei needs a place it can *write* CUE safely. `registries/` is a
crei-owned sub-package (precedent: crei already owns/rewrites
`crei.state`; cf. `go.mod`, `package-lock.json`). Inside it crei rewrites
freely; everything else is hand-authored and crei only reads it.

- `crei init` scaffolds `registries/` (empty) and the import into the root
  package. Owned-by-crei, not mandatory to use: an image not in a registry
  is simply untouched.
- It is a separate CUE package (a directory is a package), so it is
  imported and referenced qualified: `import reg "…/registries"` then
  `reg.images.gluetun`. That import cost buys the clean ownership boundary.
- First crei-written, git-tracked CUE (crei.state is runtime-side, often
  ungit'd). A `crei image pin` bump shows up as a reviewable
  `registries/images.cue` diff — the write-back loop, not runtime mutation.
- Round-trip discipline: entries carry hand-authored intent (track,
  policy) plus the crei-managed digest; `pin`/`update` regenerate the file
  canonically (`cue fmt`), preserving intent and refreshing digests. Treat
  it like `go.mod` — semi-generated, no load-bearing comments.
- Phase 3 adds `registries/secrets.cue` for managed-secret metadata
  (generation/rotation/sync, building on secrets prune/adopt/label). Shape
  the package now with that generalization in mind; build it later.

## `#ImageRegistry`

An entry is a struct: required `image` (the tracked channel), optional
`digest` (crei-managed) and policy (`minAge`, …), and a computed `#ref`.
`#ref` earns the hidden-handle convention (`#self`/`#ref`/`#service`)
because it is genuinely derived — `image@digest` — not source data; the
container reads it, never the source fields:

    Container: Image: reg.images.gluetun.#ref

Containers reference the handle; crei resolves it at eval time, like
`secrets.x` / volume `#self`. Inline image refs stay legal but lint as
`image/unmanaged` (they receive no updates).

## Policy

Attaches per-entry (struct form) or globally (`.crei/config.toml`):

- **min-age**: do not offer a digest whose image `created` is younger than
  N days (dodges yank-and-repatch churn). crane reads the config's
  `created`.
- **implicit tracking** (shipped): a version-shaped tag (optional v +
  dotted numerics, any component count — semver and CalVer alike) tracks
  ">= current" by default: the tag states the floor. `range` narrows it
  (Masterminds constraint over the first three components; full tuple
  ranks); `range: "=x.y.z"` freezes the tag (its digest still follows
  re-pushes). Floating tags (latest, stable) follow their digest.
  Selection guards: candidates must carry the identical -suffix
  (compatibility, never crossed), be at least as precise as the current
  tag, and not be a date-stamp tag (alpine's 20260127 never beats 3.20).
- **min-age is information, not a gate** (redesigned with the picker):
  candidates younger than the threshold are marked and start unselected;
  choosing one is the override. No holds, no starvation, no bypass flag.
- **lock is the gate** (shipped), and the deliberate counterpart to the
  above: `lock: {reason, since}` holds an entry where it is and records
  *why*. While set, crei rewrites nothing about the entry (update will
  not offer it, pin skips it, add --force refuses). The reason is
  surfaced by update and outdated whenever an update is waiting, because
  the failure mode is remembering the ban but not its cause months
  later; `since` dates the hold so a stale one reads as stale.
  Crucially, naming a locked entry on the update command line does *not*
  override it (naming does override a min-age marker): a lock is a
  decision made when you had context you now lack, so `crei image
  unlock` is the only way past. Stronger than `range: "=x.y.z"`, which
  freezes the tag but lets the digest follow a re-push; a lock freezes
  both.
- **CVE thresholds** (phase 2): "adopt under min-age if it fixes a CVE",
  "flag a pinned digest with a known vuln". Needs a scanner (Trivy/Grype)
  or OSV — a separate data source, deferred.

## Registry client

Vendor `go-containerregistry` (crane): self-contained (no host skopeo
dependency), handles auth via the docker keychain, digest resolution
(`crane.Digest`), tag listing (`crane.List`), and the image config for
`created`. Consistent with crei's embed-everything posture.

## Commands (`crei image …`, matching `crei secrets`)

- **`crei image add [name] <ref>`** — add and pin in one step. Name
  derived from the last repo segment (sanitized to a CUE identifier,
  `home-assistant` → `home_assistant`) unless given explicitly. A pasted
  digest is used as-is; a tag is resolved now; no tag adds unpinned.
  Collision errors (pin refreshes; `--force` replaces).
- **`crei image pin`** — fill the gaps: entries with a tag but no digest
  get pinned. Already-pinned entries are left alone — npm-install-style
  bootstrap, idempotent, no policy involved. (Migration gap: digest-only
  entries need their tag re-added by hand before they can be managed.)
- **`crei image outdated`** — read-only drift report, honoring policy
  (min-age holds; ranged entries report in-range tag advances).
- **`crei image lock <name> <reason…>` / `unlock <name…>`** — hold an
  entry at its current pin, recording why. `lock` stamps the date;
  `unlock` clears it. See the lock policy above.
- **`crei image update [name…]`** — the verb that moves existing pins:
  finds every candidate (tag advances + digest moves) and presents an
  interactive picker (charm/huh multiselect) with old->new, release age,
  and digest; young candidates are marked and unselected. `-y` skips the
  picker and applies aged candidates (naming an entry overrides its
  youth); non-TTY without -y errors with guidance. Selection is written
  back through the config.

## Lint config (needed alongside this)

The `image/unmanaged` lint is the first rule where "I deliberately didn't
manage this" is a normal state, so it must be silenceable — and that
retrofits a gap: the graph rules (`pair-cardinality`, `orphan-network`,
`duplicate-router`) currently can't be silenced either.

Unify it: **every rule has a name; config sets its severity per name**
(`error` / `warn` / `off`) — the eslint/golangci-lint shape. `#checks`
helpers already carry names; graph rules get `graph/…` names; the image
lint is `image/unmanaged`.

    [lint]
    "graph/orphan-network" = "off"
    "image/unmanaged" = "warn"

Global-by-name in `.crei/config.toml` is the MVP; inline per-unit
overrides (`//nolint`-style, or a `#lint` field) can follow. Build this
*with* the image lint, so the lint ships already-configurable.

## Phasing

1. `#ImageRegistry` + `registries/` scaffold + `crei image pin`/`outdated`
   (vendored crane, min-age) + named rules & `[lint]` config covering
   `image/unmanaged` and the existing graph rules.
2. `crei image update` write-back; semver-range policy.
3. CVE thresholds (scanner integration).
4. `registries/secrets.cue` + secret management.
