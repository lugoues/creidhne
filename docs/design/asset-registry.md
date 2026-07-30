# Asset registry (build-context files from disk)

Status: shipped.

## Problem

Build contexts took only inline content, so a large or numerous file set
(grafana dashboards, provisioning trees — thousands of lines of JSON) had
to be pasted into CUE. CUE's `@embed` was considered and rejected:

1. No recursive glob (`**`), so nested trees need one line per directory.
2. Content flows through the evaluator on *every* command that loads the
   project (status, lint, graph included), not just render.
3. Embedding is an opt-in interpreter that crei's evaluator deliberately
   does not enable (the golden harness depends on `@embed` never being
   force-evaluated), and it is text-oriented — binary assets don't fit.

## Mechanism

`#AssetRef` (`{asset: "<project-relative doublestar glob>"}`) is a third
form of build-context entry, alongside inline strings and
`{content, mode}`. At load (`eval.expandAssetContexts`, in
`LoadAndValidate` **before** `injectBuildHashes`), crei resolves the glob
with doublestar (`**` supported), reads the matched files, and expands the
ref into ordinary inline entries. Everything downstream — render, the
build content hash, state, reconcile pruning of `images/`, the golden
harness — sees plain context entries and needs no asset awareness.

Ordering before the hash injection is the load-bearing part: the file
*bytes* land in the build data, so an asset edit moves the `.build` file
and flags every consuming container stale, exactly like an inline edit.
Doing this any later would recreate the build↔container staleness
disconnect fixed for tag references.

Mapping and safety:

- The glob's static prefix is the root; each file lands at
  `<Context key>/<path relative to prefix>` (`"."` keys the context root).
- Matches are sorted; output and hashes are deterministic.
- Globs must be project-relative; absolute paths and `..` are rejected.
- A glob matching nothing is a **load error** — a typo'd glob silently
  shipping an empty dashboards dir is the failure mode this exists to
  prevent.
- Executable files keep their exec bit (mode 0755, else 0644).
- A resolved file colliding with an existing inline entry is an error,
  not a silent overwrite.

## The registry

`registries/assets.cue` holds named globs behind the usual handle
convention:

    assets: creidhne.#AssetRegistry & {
        grafana_dashboards: source: "assets/grafana/dashboards/**/*.json"
    }

    // in a quadlet:
    #build: Context: dashboards: reg.assets.grafana_dashboards.#ref

The registry is pure CUE sugar — the `#ref` value (`{asset: source}`)
unifies into the Context entry and flows through the manifest; crei has no
Go-side loader or commands for it. **Unlike images and secrets, this file
is hand-authored: crei reads it but never rewrites it** (there is no
write-back operation), so comments there are safe. `crei init` scaffolds
an empty one. An inline `creidhne.#AssetRef & {asset: "..."}` without the
registry works identically; the registry buys naming and cross-quadlet
reuse.

## Scope: contexts only, deliberately

Asset refs work only in build contexts, not volumes/mounts. crei's builds
are deterministic — the context pins what is in the image. A bind mount
would put undeclared, mutable host state behind the same handle and break
that property. If mount-shaped assets ever happen they will be a distinct
feature with distinct semantics, not an extension of this one.

## Notes / limits

- Asset bytes are read on every project load and carried in memory
  (they're part of the hashed build data). Fine for config/dashboard
  scale; not intended for gigabyte blobs.
- Binary files are safe (content never passes through JSON or the CUE
  evaluator — Go strings are byte-transparent).
