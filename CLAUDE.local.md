# Foundational Rules

## Core Principles
    Intellectual honesty: Share genuine insights without unnecessary flattery or dismissiveness
    Critical engagement: Push on important considerations rather than accepting ideas at face value
    Balanced evaluation: Present both positive and negative opinions only when well-reasoned and warranted
    Directional clarity: Focus on whether ideas move us forward or lead us astray
    Personification: Donot personify yourself, you aren't alive, you don't have feelins, don't pretend to be human

## UNDERCOVER MODE — CRITICAL
NEVER include in commit messages or PR descriptions:
- The phrase "Claude Code" or any mention that you are an AI
- Any hint of what model or version you are
- Co-Authored-By lines or any other attribution

Write commit messages as a human developer would — describe only what the code
change does.

GOOD:
- "Fix race condition in file watcher initialization"
- "Add support for custom key bindings"
- "Refactor parser for better error messages"

BAD (never write these):
- "Fix bug found while testing with Claude "
- "1-shotted by claude-opus-4-6"
- "Generated with Claude Code"
- "Co-Authored-By: Claude Opus 4.6 <…>"

## Communication Guidelines
### General
  • be direct, and ruthlessly honest
  • No pleasantries or social niceties
  • No emotional cushioning
  • no unnecessary acknowledgments
  • When I am wrong, tell me immediately and explain why
  • Verify facts against current sources
  • Use plain language
  • When nuance matters, outline trade-offs so I can choose what fits
  • When my ideas are inefficient or flawed, point out better alternatives
  • Don't waste time with phrases like 'I understand' or 'That's interesting.'
  • Never apologize for correcting me
  • Challenge my assumptions when they're wrong.
  • When discussing solutions, don't ask to implement when we are researching/planning

### Explanation Strategy
• Quality of information and directness
• If you don't now something, say so and don't guess
• Always search the internet for the latest information

### Style Rules
• No em dashes (use commas, periods, or parentheses)
• Maintain consistent headers, bold cues, and compact bullets
• If you can't write to disk then print the code in chat

### Approach
• Think deeply before answering, then deliver focused guidance.
• Always search the internet to back up your assertions before you make them

### Codex review loop
After completing a substantive feature or fix (not trivial doc/typo commits), run a codex review of the commit and resolve what it finds:

    mise exec -- codex exec review --commit <sha>    # or --uncommitted / --base main

- Triage each finding: fix valid ones (regression test first when practical, as its own commit), push back with reasoning on invalid ones rather than blindly complying.
- Re-run the review on the fix commit until it comes back clean.
- `codex` is a mise tool (`npm:@openai/codex`); `--commit` takes no prompt argument.
- Commit conventions still apply: commit as you work without asking, never push, explicit paths only (never -a/-A/.), one summary sentence, no body.

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

**Creidhne** is a **Go CLI** (binary `crei`) that generates [Podman Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html) systemd unit files from typed, validated [CUE](https://cuelang.org/) definitions, then reconciles them against a quadlet directory (plan/diff/apply). The binary **embeds the CUE evaluator** (`cuelang.org/go`) and the CUE schema, so end users need only the single binary — no `cue`, `deno`, or `mise` at runtime.

The single release artifact is the **`crei` binary** (GitHub Releases via GoReleaser). It carries the CUE schema *and* templates embedded — there is no separate package to publish. Users still `import "github.com/lugoues/creidhne@v0"`, but that resolves from the binary's embedded schema (at runtime) and an on-disk vendored copy (for the editor), never a registry.

The surface has grown well past render/apply: it also records what it applied (`crei.state`), reports runtime and staleness (`status`, `restart`, `logs`, `diff --stale`), enforces named lint rules across the whole project graph, manages an image registry with digest write-back, manages podman secrets, vendors CUE helper modules offline, and imports docker-compose projects. Each of those is its own section below.

> History: the engine was originally a set of Deno scripts run through `mise` that shelled out to `cue export`. That was replaced by this Go binary. If you find references to `mise/tasks/quadlets` or `output.files`, they are stale.

## Commands

Dev toolchain (`go`, `cue`, `golangci-lint`) and tasks are both managed by [mise](https://mise.jdx.dev/) (`.mise/config.toml`), the single task runner. Use `mise exec -- go ...` (or activate mise) since `go` isn't on the bare PATH.

```sh
mise run build      # go build -o bin/crei ./cmd/crei
mise run test       # go:test (go test -v ./...) + cue:vet (cue vet in creidhne/)
mise run cue:vet    # validate the schema module only
mise run lint       # golangci-lint run
mise run gen        # regenerate the systemd [Unit]/[Service]/[Install] schema + template
mise run tidy       # go mod tidy
mise run snapshot   # goreleaser release --snapshot --clean (local release dry-run)
```

Go tests directly:

```sh
mise exec -- go test ./...                                                # all
mise exec -- go test . -run TestGolden/build-basic -v                     # one golden case
mise exec -- go test ./internal/reconcile/ -run TestPruneEmptyDirs        # one reconcile test
```

The binary (after `mise run build`, it's at `bin/crei`). Every command takes the persistent `-C`/`--dir`, `--quadlet-dir`, `--diff-tool` flags:

```sh
# core pipeline
crei init                       # scaffold cue.mod, sample, registries/, .crei/, vendored schema
crei render [quadlet...]        # render unit files to stdout
crei plan                       # dry-run diff against the quadlet dir
crei diff [--stale]             # detailed diffs (--stale: running config vs applied)
crei apply [-y] [--no-diff] [--reload-systemd]   # write/remove, prune images/, record crei.state
crei validate                   # strict whole-package type check, no rendering
crei config                     # resolved config plus the source layer of each value

# observability & lifecycle
crei status [quadlet...] [--problems] [--check] [--format table|json]
crei start [quadlet...] [--all]  # runnable leaves; deps start via systemd; idempotent
crei stop [quadlet...] [--all] [-y]
crei restart [quadlet...] [--stale]
crei logs [quadlet...]          # journalctl passthrough
crei graph [quadlet...] [--format dot|mermaid|json] [--flat]
crei lint [quadlet...]          # named rules: deps/*, graph/*, image/*

# registries & helpers
crei image add|pin|outdated|update
crei secret create|rotate|remove|list|prune|adopt   # create registers in registries/secrets.cue too
crei vendor [module[@ref]] [--check]
crei import compose [file|url...]
crei version
```

## Configuration

Config file is **`.crei/config.toml`** inside the project dir (not a root `crei.toml`, that spelling is stale). `crei init` also writes `.crei/config.schema.json` (the embedded `crei.schema.json`) and a `#:schema` directive, so Even Better TOML / Taplo validate it offline; the schema file is rewritten on every `init` so it tracks the binary.

Precedence: flags (`--quadlet-dir`, `--diff-tool`) > env (`QUADLET_DIR`, `DIFF_TOOL`) > `.crei/config.toml` > defaults (quadlet dir `~/.config/containers/systemd`). `crei config` prints the winner and its source for each key. A malformed config is a hard error, never silently ignored, so a typo can't quietly route `apply` at the default directory.

Keys: `quadlet_dir`, `diff_tool`, `diff_style` (`highlight`/`plain`/`inline`), `reload_systemd` (default true, matching `podman quadlet install`), `secrets_field` (default `secrets`), `restart_timeout` (default `60s`; how long restart/start/stop wait on a unit still transitional after its systemd job cleared, `0` waits forever), the `[style]` table (per-element lipgloss colors for headers/context/add/remove/inline spans, hex or ANSI index, validated on load), and the `[lint]` table (rule name to `error`/`warn`/`off`).

The path is the `configRelPath` constant in `internal/cli/root.go`, used for the loaded path, the source label `crei config` prints, help text, and error messages alike, so the documented name can't drift from the loaded one again (it did: help text said "crei.toml" long after the file moved under `.crei/`). Use the constant, never a literal. `config_test.go`'s legacy-location test deliberately keeps the literal `crei.toml`, since there the old name being *ignored* is the assertion.

## Architecture

### The generation pipeline (the core data flow)

Trace it across packages:

1. **User CUE** — top-level `#Quadlet` values that `import "github.com/lugoues/creidhne@v0"` (see `example/`).
2. **`internal/eval`** — `LoadAndValidate(dir, overlay)` uses `cuelang.org/go/cue/load` to evaluate the project, then extracts each `#Quadlet`'s **`manifest`** field into `[]Quadlet{Name, Units[]}`. Crucially it decodes the unit `data` JSON with a **`json.Number`→`int64` coercion** so templates' `{{ printf "%d" }}` render integers, not `%!d(float64=N)`. (`eval.go`)
3. **`internal/render`** — `New(tplFS)` parses the embedded `.tpl` files; `BuildFileSet([]Quadlet)` runs each unit's `data` through the matching Go `text/template`, producing a `filename → {Content, Mode}` map. Build units also emit `images/<stem>.Containerfile` and `images/<stem>.context/<path>` (Context entries normalized: plain string → mode `0644`, else `{content, mode}`). (`render.go`)
4. **`internal/reconcile`** — `ComputePlan(desired, dir)` diffs against disk (add/change/unchanged/remove); apply writes/removes with **plain filesystem ops (never escalates privileges)**, prunes empty `images/` dirs, optional `systemctl daemon-reload`. (`reconcile.go`)
5. **`internal/cli`** — cobra commands wire config → `eval` → `render` → `reconcile`, with output/exit-code parity to the old Deno tasks. (`root.go`, `commands.go`, `ergonomics.go`)
6. **`internal/state`** — `apply` records what it wrote (`crei.state` in the quadlet dir): the evaluated manifest plus a hash per file. This is the *recorded* layer between desired CUE and on-disk files (the kubectl last-applied analogue) that `status` needs to tell a pending edit (desired != recorded) from tampering (recorded != disk).

`internal/kinds` is the single source of truth for the unit kinds and their extensions; both `render` (which template, what filename) and `reconcile` (what it manages and prunes) derive from it, so they cannot disagree. A kind known to one but not the other would be written and never pruned, or vice versa.

### The CUE↔Go boundary: `manifest` (Go renders, not CUE)

The key design decision: **rendering moved out of CUE into Go.** CUE only *validates and exports data*; Go owns the templates. This means the binary's embedded CUE evaluator never needs the `@embed` or `@experiment(try)` CUE features.

`cue export` of a `#Quadlet` drops hidden fields (`#container`, `#ref`, `#service`), so `creidhne/quadlet.cue` exposes a non-hidden **`manifest` list** that *promotes* each unit's computed `stem`/`#ref`(→`filename`)/`#service`(→`service`) alongside its `data`. Go reads the manifest and never touches a `#`-field. `#ref`/`#service`/`stem`/`secretStrings`/build-context **stay computed in CUE** (cross-quadlet refs like `After=app.service` resolve at eval time into `data`); the manifest only *surfaces* them.

### The CUE schema module (`creidhne/`) is data-only

`creidhne/` is the schema module (import path `github.com/lugoues/creidhne@v0`, matching the directory name). It contains **only** type definitions + validators — `render.cue` and `templates/templates.cue` were deleted; the `.tpl` files moved to the repo-root **`templates/`** dir, which **Go owns** and embeds. The `.tpl` files are unchanged Go `text/template` syntax — only the *executor* changed (CUE's builtin → Go's stdlib).

### Embedded schema + offline overlay

`assets.go` (root package `creidhne`) `//go:embed`s `creidhne/` (schema) and `templates/*.tpl`. `eval.Overlay(moduleRoot, SchemaFS)` builds a `load.Config.Overlay` that vendors the schema under `<moduleRoot>/cue.mod/usr/github.com/lugoues/creidhne/...`, **mirroring the repo's on-disk symlink layout** — so a user's `import` resolves offline, version-locked to the binary. `crei init` writes that schema to disk (`vendorSchema`) so the editor LSP / `cue` CLI resolve it for authoring, and **every project command re-syncs it** (`syncVendoredSchema` in `loadQuadlets`) when it drifts from the embedded copy (byte comparison) — so after a binary upgrade the editor stays in lockstep with no manual step. Sync is best-effort and only refreshes an *existing* vendored copy (projects that resolve the schema another way are left alone). (`//go:embed` can't traverse `..`, which is why `creidhne/` and `templates/` sit at repo root and are embedded by the root package.)

### Subsystems beyond the pipeline

**State, status, staleness.** `apply` writes `crei.state` (its extension isn't a managed quadlet extension, so `reconcile.ListExisting` never sees it and quadlet ignores it). `status` classifies each unit across four layers (desired CUE, recorded state, actual disk, systemd runtime); `internal/systemd` batches one `systemctl show` for the runtime column and is **strictly read-only** (reconciliation never consults it: files are the substrate, systemd's view is generator output derived from them). Staleness tiers 1-2 are shipped (applied-vs-running history in `crei.state`, `status` annotations, `diff`/`restart`/`logs --stale`); `stale.go` also knows when a restart *cannot* apply a change, e.g. a `.volume` whose `podman volume create --ignore` no-ops against an existing volume.

**Named lint rules.** `internal/cli/lintconfig.go` holds `ruleDefaults`, the registry of every Go-enforced rule and its default severity: `graph/pair-cardinality`, `graph/pair-unwired`, `graph/duplicate-name`, `graph/orphan-network`, `graph/duplicate-router` (`graphrules.go`), `deps/redundant-resource`, `deps/redundant-network-online` (`lint.go`), `image/unpinned` and `image/unmanaged` (`imagerules.go`, the latter `off` by default since not using the registry is a supported choice). `[lint]` in the config overrides per name; an unknown rule name or invalid severity is a hard error so a typo can't silently leave a rule at its default. `lint` reports them all, and `validate`/`plan` also run them. `#checks` defined in CUE are separate: they are schema constraints that fail evaluation and cannot be softened here.

**Graph rules are keyed by marker labels**, not by helper identity: `creidhne.pair=<name>` is the whole contract, so a rule needs no knowledge of the helper (e.g. extras' `#ReverseProxyMixin`) that placed it. These prove the project's *declared* graph, not runtime state; out-of-project attachers are invisible.

**Asset registry.** `registries/assets.cue` names project-file globs (`#AssetRegistry`, doublestar `**` supported) consumed as build-context entries via `reg.assets.<name>.#ref`. Pure CUE sugar: the `#ref` (`{asset: glob}`) unifies into `Context` and `eval.expandAssetContexts` (in `LoadAndValidate`, **before** `injectBuildHashes` — ordering is load-bearing so asset bytes join the build hash) expands it into inline entries; render/state/reconcile need no asset awareness. Empty glob = load error; contexts only, never mounts (determinism). Unlike images/secrets this file is **hand-authored — crei never rewrites it**. See `docs/design/asset-registry.md`.

**Image registry.** `registries/` is a crei-*owned* CUE sub-package holding `#ImageRegistry` entries (`image` = the hand-edited tracked channel, `digest` = the crei-written pin, `#ref` = computed `image@digest`). `internal/registry` is the only place with network access (vendored `go-containerregistry`/crane plus Masterminds semver); `eval.LoadImageRegistry` decodes the entries; `crei image add|pin|outdated|update` write back through `emitImageRegistry`, so every image bump is a reviewable config diff rather than runtime mutation. A version-shaped tag implicitly tracks `>= current`; `range` narrows it, `=x.y.z` freezes it, floating tags follow their digest. min-age is *information* (a marker on young candidates in the picker), not a gate. See `docs/design/image-registry.md`.

**Build content hash.** `eval.injectBuildHashes` stamps `creidhne.build-hash` (a hash of the build's pristine inputs: Containerfile, context, BuildArg, ImageTag) onto the build unit *and* every container consuming the built image, so a context edit moves the `.build` file and flags dependent containers, which crei otherwise couldn't see (it tracks no image identity). Runs once over the whole project, so every render subset sees identical already-stamped data. See `docs/design/build-content-hash.md`.

**Error translation.** `internal/eval/diag.go` collapses cue's disjunction noise, humanizes contract paths, names the schema's constraints, and suggests fixes for typos, surfacing a `DiagnosticError` that `cli.printDiagnostic` styles (actionable line loud, positions and raw detail dim). Matchers are deliberately conservative: anything unrecognized falls through, and the raw output is *always* appended. See `docs/design/error-translation.md`.

**Vendoring helper modules.** `crei vendor <module>[@ref]` fetches a git-hosted CUE helper module into `cue.mod/usr/<module-path>/` (the same offline layout the embedded schema uses) and pins commit + tree hash in `cue.mod/crei-vendor.json`. A vendored module may import the CUE stdlib, itself, the creidhne schema, and modules already in the vendor lock; crei never fetches deps transitively (vendor a module's dependencies first). `--check` verifies offline and exits non-zero on drift.

**Importer.** `internal/importer` converts a docker-compose project into one `#Quadlet`: services become containers, named volumes/networks become units referenced through `#self` handles, build sections become build units, compose secrets map onto `#SecretRegistry`. Anything unrepresentable is reported, never silently dropped.

**Secrets.** `crei secret create|rotate|remove|list|prune|adopt   # create registers in registries/secrets.cue too` reads the project's `#SecretRegistry` (top-level field named by `secrets_field`) and drives podman through `internal/podman`, which shells out to the `podman` binary (crei never links libpod), mirroring how reconcile shells out to `systemctl`.

**Generated systemd sections.** `creidhne/systemd_sections.gen.cue` and `templates/systemd_sections.gen.tpl` are generated by `mise run gen` (`tools/gen-systemd-sections`) from systemd's own pinned `load-fragment-gperf.gperf.in` parser table, plus man-page DocBook XML for the doc comments. Never hand-edit them. Enum *values* aren't in that table, so enum-typed directives map to curated types in `creidhne/types.cue`; unmapped parsers fall back to `string` and are reported to stderr.

### Package layout

```
cmd/crei/                 # main → cli.Execute()
assets.go                 # root pkg `creidhne`: //go:embed creidhne + templates + crei.schema.json
internal/eval/            # cue/load + overlay + manifest decode + number coercion,
                          #   build hashes (buildhash.go), image registry (images.go),
                          #   error translation (diag.go), #checks resolution
internal/render/          # text/template execution + build artifacts
internal/reconcile/       # plan/diff/apply, prune (port of the old lib.ts)
internal/kinds/           # single source of truth: unit kinds ↔ file extensions
internal/state/           # crei.state: last-applied manifest + per-file hashes
internal/systemd/         # read-only `systemctl show` for status/restart
internal/podman/          # podman CLI wrapper (secrets)
internal/registry/        # OCI ref parsing, digest/tag/created lookups (crane), semver
internal/importer/        # docker-compose → creidhne CUE
internal/cli/             # cobra commands, config, styling, lint rules, output
creidhne/                 # CUE schema module (data + validators only)
templates/                # the .tpl files (Go-owned, embedded)
tools/gen-systemd-sections/  # generator for the systemd_sections.gen.* pair
testdata/                 # CUE golden fixtures (separate module; schema via embedded overlay)
example/                  # realistic consumer project
docs/design/              # design docs; backlog.md is the queue of next work
crei.schema.json          # JSON Schema for .crei/config.toml (embedded, written by init)
```

### Reconcile safety invariants (load-bearing)

`reconcile.ListExisting` returns **only files** with a managed extension (`.container/.pod/.volume/.network/.kube/.build/.image/.artifact`) plus the recursive `images/` subtree — **never directories**. This is what prevents a stale directory being scheduled for `rm -rf`. `ensureDir` handles the stale-file→directory transition; `PruneEmptyDirs` walks bottom-up and never removes the root. These have explicit regression tests in `reconcile_test.go` — preserve them.

## Testing

- **Go golden tests** (root `golden_test.go`, `package creidhne_test` — the module's integration test): render every `testdata/<case>` fixture through the **embedded** templates + schema (resolved via an overlay built from `SchemaFS`) and the eval/render packages, asserting **byte-equality** against its `expected/` tree (read from disk, including the `images/` subtree). It lives at the repo root so it reaches the fixtures without `../..` traversal. Replaced the old `cue vet`-unification mechanism. (The fixture module stays `github.com/lugoues/quadlets-test` with a now-stale `cue.mod/usr` symlink — its `cue.mod/` is permission-locked, uid 1000 — but that's moot since the schema comes from the overlay, not the symlink.)
- The fixtures still carry `expected: {@embed ...}` blocks, but the Go loader **never force-evaluates `test.expected`** (it only reads `test.subject` / top-level `#Quadlet` values), so `@embed` is never triggered and no fixture changes were needed.
- **Reconcile unit tests** (`internal/reconcile/reconcile_test.go`): mirror the old `lib_test.ts`, especially the directory-exclusion and file→dir-transition safety cases.
- **Overlay test** (`internal/eval/overlay_test.go`): proves an end-user project resolves the **embedded** schema purely from an overlay (offline).
- **`internal/eval` unit tests** carry most of the schema-behavior coverage: `diag_test.go`/`errors_test.go` (error translation), `checks_test.go` (`#checks` resolution), `flatten_test.go`, `secrets_test.go`, `self_test.go`, `naming_test.go`, `validators_test.go`, `importforms_test.go`, `jsonlabel_test.go`, `decode_test.go`.
- **`internal/cli` tests** cover the command surface without a live podman/systemd: config precedence and styling, confirm prompts, lint rules and `[lint]` config, graph output, status/stale classification, image add/pin/update, vendor, import.
- **`internal/podman/integration_test.go`** needs a real podman and is skipped without one; everything else stubs the CLI through the package's `run` var.
- **`cue vet`** still validates the schema module (`mise run cue:vet`) and fixture inputs against the schema; Go owns *output* assertions.

## Conventions & gotchas

- **Adding/changing a unit field is still two edits**: the validator in `creidhne/<type>.cue` *and* the Go template in `templates/<type>.tpl`. A field in the schema but not the template validates yet renders nothing.
- **Adding a whole unit type** touches: the new `creidhne/<type>.cue`, `templates/<type>.tpl`, a `manifest` comprehension entry in `creidhne/quadlet.cue`, and the `ext` table in `internal/kinds/kinds.go` (render and reconcile both derive from it, so this is one edit, not two).
- **Never hand-edit** `creidhne/systemd_sections.gen.cue` or `templates/systemd_sections.gen.tpl`; change `tools/gen-systemd-sections` and run `mise run gen`.
- **Adding a Go-enforced rule** means registering it in `ruleDefaults` (`internal/cli/lintconfig.go`) with a default severity; an unregistered name in `[lint]` is a hard config error, and an unregistered finding gets no severity.
- The **`manifest` contract** in `creidhne/quadlet.cue` is the CUE↔Go interface. If you change a record's shape, update `eval.tryQuadlet` decoding to match.
- `#ref`/`#service`/`stem`/`secretStrings` and build-Context normalization stay in CUE / are surfaced via the manifest — don't recompute them in Go (two sources of truth for cross-ref strings = drift).
- Value validators (bytes, durations, ports, signals, enums) live in `creidhne/types.cue`; reuse them.
- **The tool never elevates privileges.** Writing to a root-owned dir (e.g. `/etc/containers/systemd`) requires the user to run `sudo crei apply`; on a permission error the CLI returns a hint to do so. (This deliberately drops the old Deno engine's internal `sudo` shelling.) `daemon-reload` scope (`--user` vs system) is chosen by whether the quadlet dir is under `$HOME` — a path heuristic, not a privilege check (`underHome` in `internal/cli/root.go`).

## Sibling repos checked out in this workspace

Both are **separate git repos**, not part of this module. Don't commit them from here.

- **`creidhne-extras/`** (`github.com/lugoues/creidhne-extras`) is the CUE helper module (`github.com/lugoues/creidhne-extras@v0`): mixins and spec types built *on* the schema (`reverse-proxy.cue`, `gluetun.cue`, `subnet.cue`, `internal-network.cue`, `static-network.cue`, `docktail.cue`, `borg-manager.cue`, `build.cue`, `utilities.cue`). It's consumed via `crei vendor`, and it's the main real-world exercise of `#checks`, the `creidhne.pair=` marker contract, and list flattening. Schema changes here usually want a matching check against extras.
- **`borgmatic-manager/`** (`github.com/lugoues/borgmatic-manager`) is a separate Go project, paired with extras' `borg-manager.cue`.

## Ignore these (untracked local leftovers)

`todo.md`, `notes..md`, `nohup.out`, `session-export-*/`, the stray root `-` file, the stray built `gen-systemd-sections` binary (the source is `tools/gen-systemd-sections/`), and `bin/` are scratch/debug artifacts, not part of the project. The live fixtures are in `testdata/`.

Note `docs/design/backlog.md`, `image-registry.md`, and `cue-0171-interpolation-regression.md` are currently **untracked** but are real design docs, not leftovers.
