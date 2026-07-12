# Design: transactional apply, generations, and crei-managed auto-update

Status: draft, revised after two review passes (including an adversarially-verified
one). Nothing here is built yet.

This captures the design conversation around giving `crei` a rollback story that
spans services, plus a crei-owned auto-update path. It records decisions, the
reasoning behind them, the genuinely hard parts, and the open questions still to
resolve.

Scope statement up front, because it is load-bearing: **rollback restores the
file + image layer (unit definitions and image identity). It does not restore
data.** Volume mutations (most importantly forward, non-backward-compatible schema
migrations) cannot be undone by this mechanism. See section 11. This is the same
line OSTree, transactional-update+snapper, and bootc draw (they exclude `/var`);
it is an authoritative boundary, not an apology.

## 1. Motivation

`podman auto-update --rollback` is **per container**: it pulls a new image,
restarts that unit, and reverts that one container to the prior digest if the
restart fails to signal ready. Note how weak that signal is by default: a
Quadlet-generated unit uses `sdnotify=conmon` unless it sets `Notify=true`/`healthy`,
and conmon emits `READY=1` when the container *process starts*, not when the app
is ready, so a forked-but-not-ready (or immediately-exiting) container is reported
as a successful restart and is **not** rolled back. `crei apply` changes **many
units at once**. The gap we want to close: if applying a set of changes makes
*any* service in the set fail (including a downstream service that was not itself
changed), roll the **entire plan** back to the last known good file+image state.

Secondary goal: absorb useful ideas from [materia](https://github.com/stryan/materia)
(GitOps for quadlets) without becoming materia. We are not replacing it.

## 2. Goals and non-goals

Goals:
- Atomic multi-service `apply`: any failure rolls the whole plan back (file+image
  layer), **except** plans containing units unsafe to revert, which escalate to
  halt-and-alert (section 11).
- Durable generations so a prior good state can be restored on demand.
- A crei-owned `auto-update` that refreshes images on a schedule with the same
  cross-service transactional rollback.
- Stay a single binary. No new heavyweight dependencies, **no second external
  binary** (podman CLI only, no skopeo as a hard dep), no daemon, no git, no
  podman REST bindings.

Non-goals (explicitly deferred, per discussion):
- **A daemon.** Only justified for (1) auto-sync from a remote repo / C2, or (2)
  continuous reconciliation that forces live state to match config (k8s-style).
  Neither is needed now. Everything below is one-shot, optionally timer-driven.
- **Replacing podman auto-update for fire-and-forget users.** A unit can opt out
  of crei image management and keep a floating tag for podman to own (D6).
- **Rolling back data.** Out of scope and not solvable here (section 11).

Risk posture: the manual rollback core is low-risk and proven; T3
auto-rollback-on-health and unattended auto-update are the risky surfaces and are
sequenced last, opt-in, and (for auto-update) experimental. See sections 11, 12,
and 14.

## 3. Background: what `crei apply` does today

1. Eval CUE, render unit files, compute a plan (`[]reconcile.Change`:
   Add/Change/Remove/Unchanged, each carrying new content + mode).
2. Apply removals first (so a file<->dir transition is safe), then writes, with
   plain filesystem ops. Never escalates privileges.
3. Prune empty `images/` dirs.
4. Optional `systemctl daemon-reload` (scope by whether the quadlet dir is under
   `$HOME`).

It does **not** touch service lifecycle (no start/restart/enable) and does not
read podman runtime state. A mid-apply failure today leaves a **partially
applied** directory with no rollback. The safety invariant that makes the rest of
this tractable: the reconciler only ever touches the *managed set* (files with a
managed extension plus the `images/` subtree), never unmanaged files, never bare
directories. The managed kinds are `.container .pod .volume .network .kube .build
.image .artifact` (section 10).

## 4. Decisions

### D1. Sidecar generations store, not git

Git was considered as the journal/rollback mechanism and rejected: `git reset
--hard`/`git clean` blast the whole tree (breaking the managed-set invariant), git
records only the exec bit (the `images/*.context/*` entries can carry non-644
modes), it forces the root-owned quadlet dir to be a repo, and it is a large leaky
surface for "keep copies of some files."

Instead: a **sidecar generations store** outside the quadlet dir, snapshotting
exactly the managed set with full fidelity (bytes + mode), restored through the
existing safe `reconcile.WriteFile`/`RemoveFile` so it inherits every invariant.

Layout (illustrative):

```
<state_dir>/<deployment-id>/
  host                an identity sentinel (machine-id or a generated host token)
  blobs/              content-addressed (sha256 -> bytes); generations dedup for free
  generations/
    0000.json         gen-0 baseline (pre-crei state; section 5)
    0007.json         manifest (see section 5)
  current -> 0007
```

- **`deployment-id` is the canonicalized absolute path of the target quadlet dir**
  (hashed), **not** the CUE package. The quadlet dir is the unit of "deployed into
  one place" and must have one owner and one chain. The id stays **path-stable**
  (do not fold the host id into it, or legitimate same-host restore/migration where
  the path is the stable key would break).
- **Single-host ownership.** The store is host-local. A `host` sentinel (machine-id
  where available, else a generated token, because `/etc/machine-id` is empty or
  unstable in many rootless/container setups) lets crei **refuse on mismatch** when
  a shared or replicated `state_dir`/dir is opened from a different host. This is
  the honest cross-host guard; the per-scope flock (section 13) only serializes
  same-host runs (NFS lock semantics are unreliable, so flock is not a fleet lock).
- **Location is config-driven.** `state_dir` in `crei.toml`, `--state-dir`,
  `CREI_STATE_DIR` (flag > env > toml > default). Default (rootless):
  `$XDG_DATA_HOME/creidhne` (`~/.local/share/creidhne`); (system): `/var/lib/creidhne`.
  Put it anywhere **host-local**.
- **`state_dir` is rollback-critical, not disposable.** Generations, manifests,
  and `crei prune`'s image protection live only here. Losing it does not corrupt
  image identity (D3 reads the pin from the deployed file) but it **does** lose
  rollback capability and un-protects retained built images from `prune -a`.
  Recommend backing it up; warn+confirm when the quadlet dir already holds managed
  files but no chain exists (section 8). Do **not** stamp a relink sentinel inside
  the quadlet dir: that colonizes the scanned dir, the same principle that killed
  git; the warn+confirm gate covers both the dir-moved and store-lost cases.
- Content-addressed blobs make "keep all" cheap; retention is `keep last N` with
  the keyword `unlimited` for no cap (not `0`, which reads as "keep none").
  `revisionHistoryLimit=10` is the k8s precedent for the default N (section 15).

Optional, later: a `crei export` that commits rendered output into a repo the user
designates, purely as human-facing audit/history. Git for history, sidecar for the
machine undo.

### D2. Transactional apply

Every plan change has a trivial inverse (Add -> delete; Change -> restore prior
bytes+mode; Remove -> recreate). The flow has a **prepare / mutate** split:
everything that can fail expensively (image resolution, builds, and the
rollback-target image preflight) happens **before** any file is touched, so a
prepare failure is a true no-op.

Flow: **prepare (resolve images, run changed builds, preflight rollback-target
images exist) -> snapshot affected managed files -> mutate (apply) -> validate ->
on failure restore (+ re-reload), on success commit (write generation, flip
`current`).**

"Validate" tiers:
- T1 file atomicity: any write/remove error rolls back. Pure filesystem.
- T2 generation validity: after reload, assert each expected unit materialized in
  the **generator output dir** (`/run/systemd/generator/`, or
  `/run/user/$UID/systemd/generator/`), not via `systemctl status` (a quadlet the
  generator silently rejected produces *no* unit, so status would not list it).
- T3 health-gated: start/restart affected units in dependency order, gate on
  *stable* readiness vs the pre-apply baseline, roll the whole plan back in reverse
  order on regression. The risky one (sections 9, 11, 12).

T1+T2 are the default (partial apply is a latent bug), with `--no-rollback` to opt
out. T3 is opt-in. Note **T2-green does not mean the workload changed**: a digest
bump rewrites the file and the unit re-materializes, but `daemon-reload` does not
restart running units, so the old container keeps running until something restarts
it (section 9, `--restart`).

### D3. Image ownership: pin pulled images by index digest, preserve built images

The file snapshot captures the unit definition, not the running image. To make
file rollback authoritative for the image, crei owns image identity. This covers
`.container` `Image=`, `.image` (a first-class pulled image), and `.build`
`ImageTag` (section 10). `.volume Driver=image` and `.kube` (image lives in the
referenced YAML) are deferred (section 10).

- **Pulled images (`.container`, `.image`):** resolve the authored tag to **the
  digest the registry serves for that ref, unparameterized by platform**, via a
  remote inspect that does not pull layers (`podman manifest inspect docker://<ref>`;
  skopeo only as an optional accelerator, never a hard dep). Pin that digest into
  the rendered file as `Image=name@sha256:...`: the manifest-list (index) digest
  for a multi-arch image, the single manifest digest for a single-arch image.
  - **Never read `RepoDigests[0]`** or a post-pull `podman image inspect .Digest`:
    RepoDigests has no defined order, holds per-arch child digests for multi-arch
    images, and inspect-by-tag vs by-ID disagree (podman #24858, a 4->5 regression).
    A per-arch child digest wrong-architectures any migrated/cross-host deploy and
    is the entry registries GC first.
  - **Never pass `--platform`/`--override-arch`** during resolution. Abort prepare
    loudly on empty/ambiguous resolution.
- **Built images (`.build`):** no inherent registry backstop; preservation is
  mandatory (or push, section 8). See section 7.

Consequence: **re-tagging on rollback (podman's `podman tag $old $name`) is
unnecessary** for digest-pinned units. podman re-tags because it does not own the
unit file; crei generates the file, so restoring the previous generation's
digest-pinned file is enough.

**Sticky pins (when to resolve), and where the pin lives.** Resolving the authored
tag on *every* apply would make any floating tag show a diff whenever upstream
moved, so a config-only apply would drag in an unrelated image update. Resolution
is sticky: re-resolve only on (a) first deploy, (b) a changed *authored* reference
in CUE, or (c) `--pull`. The current pin is read from the **deployed file** (which
already holds `Image=name@sha256:...` and is the diff target), not the manifest, so
the no-op property survives state-dir loss and manifest/deployment drift. The
manifest still records the digest for the rollback chain.

### D4. Per-unit auto-update opt-in

You will not want everything auto-updated. Auto-update is opt-in **per unit** via
a crei-level flag in CUE intent + the manifest; it is **not** rendered into the
unit and does **not** reuse podman's `AutoUpdate=`. Stateful services should
generally stay out of the auto-update set entirely (section 11).

### D5. Auto-update's source of truth is the last generation, not the live CUE

- **Manual `crei apply`** reads the **CUE source** for config and resolves images
  per the sticky rule (D3), then writes a new generation. The only path that
  deploys config changes.
- **`crei auto-update`** (the timer) reads the **last generation**, refreshes only
  image identity for opted-in pulled images, and writes a new generation. It does
  **not** read the live CUE and (v1) does not rebuild builds (section 7).

An unattended timer that re-evaluated CUE could silently deploy un-applied edits.
Binding it to the committed generation keeps scheduled runs predictable (images,
never config). The quadlet dir stays the diff target; the generation is the
desired-config source.

### D6. podman auto-update is sidestepped

Because crei pins managed images by digest (D3), podman auto-update cannot act on
them: `AutoUpdate=registry` has no tag to compare, **and `AutoUpdate=local` cannot
act either** (local restarts only if the container's image *name* resolves to a
different ID in local storage, and a bare-digest reference always resolves to its
own immutable digest). Setting either on a crei-managed unit is a no-op; crei
**warns** but does not hard-fail.

Two render rules fall out:
- crei always writes a **bare digest** (`name@sha256:...`), **never** a tag+digest
  reference (`name:tag@sha256:...`), which podman rejects with "Docker references
  with both a tag and digest are currently not supported" (podman #21190; it fails
  only those units with exit 125, not the whole set, but crei avoids it entirely).
- **Opt-out / hand-back** is a CUE-intent flag, not an imperative command: a unit
  marked "podman-owned" is rendered with its authored **floating tag** (no digest),
  so podman auto-update can own it. There is deliberately no `crei unmanage` verb
  (it would collide with "unmanaged" = files outside the managed set, and editing
  deployed files out of band reintroduces the CUE-vs-file drift D3 avoids). De-pin
  flows through CUE -> render -> apply, one source of truth.

## 5. The generation manifest

The top-level `files` map is the authoritative `path -> {blob, mode}` record file
rollback replays; per-unit entries reference files by path and do **not** repeat
the hash. Illustrative:

```jsonc
{
  "generation": 7,
  "created": "2026-06-24T12:00:00Z",
  "crei_version": "1.6.0",
  "trigger": "apply" | "auto-update" | "baseline",   // baseline = gen-0 pre-crei state
  "outcome": "committed" | "rolled-back" | "halted" | "failed-dirty",
  "units": [
    {
      "name": "app", "kind": "container", "file": "app.container",
      "image": {
        "authored": "docker.io/library/nginx:1.25",
        "kind": "pulled",
        "digest": "docker.io/library/nginx@sha256:INDEX",   // index/manifest digest, not a per-arch child
        "auto_update": true, "no_auto_rollback": false, "podman_owned": false
      }
    },
    {
      "name": "web", "kind": "build", "file": "web.build",
      "context_blob": "sha256:CTX",          // tree hash of Containerfile + context; gates rebuild
      "image": {
        "kind": "built",
        "image_id": "sha256:LOCALID",
        "content_tag": "localhost/web:crei-CTXSHORT",   // content-addressed, NOT gen-number-scoped
        "remote": "zot.lugoues.dev/quadlets/web@sha256:...",   // if pushed (section 8); else null
        "auto_update": false, "no_auto_rollback": false
      }
    }
  ],
  "files": { /* path -> {blob, mode} for the full managed set */ }
}
```

- **gen-0 baseline.** On first apply, persist a `trigger:"baseline"` generation
  capturing the **full pre-crei managed set** (the existing step-6 snapshot is only
  "affected" files, so first-apply must widen it). Without this, after a successful
  first apply `crei rollback` could not return to the pre-crei state. Mark it
  specially: rolling back to it deletes everything crei manages and reinstates
  orphans, so `crei generations`/`rollback` treat it distinctly. First apply must
  also specify disposition of pre-existing orphan managed files.
- **No persistent "non-restorable" flag.** Built-image restorability is a live
  `podman image exists` probe at rollback time (section 7); a stored flag would go
  stale.

## 6. `crei apply` control flow (target, T3)

Prepare phase (no filesystem mutation; failures here abort cleanly):

1. Eval CUE, render desired files.
2. **Resolve or carry forward images** (D3): unchanged authored ref with a prior
   pin (read from the deployed file) carries forward; first deploy / changed ref /
   `--pull` resolves the index digest via `podman manifest inspect` and rewrites
   `Image=` to the bare digest.
3. **Run changed builds** (gated by `context_blob` delta, section 7), capture the
   image id, apply the content-tag + labels, optionally push.
4. **Preflight rollback-target images:** `podman image exists` for the images the
   prior generation would need on rollback. If an unrecoverable rollback target is
   already gone, refuse the apply **here**, before any teardown.
5. Record resolved/carried/built identities into the pending manifest.

Mutate phase:

6. Compute the plan; **snapshot** the affected managed files (full set on first
   apply, for gen-0).
7. Apply removals then writes.
8. `daemon-reload`; T2-validate via the generator output dir. (T2-green is not
   "workload changed"; see D2 and section 9.)
9. **Health gate (T3, if enabled):** start/restart affected units in dependency
   order, wait for *stable* readiness vs the pre-apply baseline.
10. **On a T3 regression:** restore the snapshot, `daemon-reload`, restart the
    prior units (reverse order). This returns the **file+image** layer to its
    pre-apply state; it does **not** undo data changes (section 11). Plans
    containing a `no_auto_rollback` unit do not partially roll back: they escalate
    to **halt-and-alert** (section 11). If the restore restart **itself** fails
    (port/name/volume still held by the half-dead new container, dependency drift,
    or a target image gone despite the preflight, since release of resources is
    unknowable until attempted), do not loop: leave files at the restored
    generation, write a **FAILED-DIRTY** marker, exit non-zero with a distinct
    alert, surface in `crei status`, require explicit `crei recover`. A failed
    rollback is a terminal state, never reported as success.
11. **On success:** write the generation (`outcome:"committed"`), flip `current`,
    prune beyond retention.

T1/T2 failures (steps 7-8) happen before any unit starts, so no migration can have
run: whole-plan file rollback there is always safe (the proven phase-1 core). Only
a post-start T3 regression (step 10) can hit the stateful escalation.

## 7. Build units (the hard case)

**Why builds are different:** no inherent registry backstop (a built image cannot
be re-pulled unless pushed); builds are **lazy** (the `<name>-build.service` runs
`podman build` when *started*, so crei must drive the build at apply, prepare-phase
step 3, inverting the laziness and making apply trigger multi-minute builds that
must abort cleanly on failure); and **rebuild is not reproducible** (apt/base/
timestamps), so rollback must reference the *preserved* old image, never rebuild.

**Approach:**
- **Content-addressed, sticky build tag, not a generation-number tag.** Tag each
  build output `localhost/<name>:crei-<short-context-hash>` (derived from
  `context_blob`), and pin the consuming `.container` to it. This is *sticky by
  construction*: an unchanged build context yields the same tag, so the rendered
  `.build` and `.container` stay byte-identical across applies and `ComputePlan`
  reports `Unchanged`. (A monotonic `crei-gen-N` tag would instead make every
  config-only apply rewrite both files, breaking the no-op-apply property at the
  file layer before any rebuild question, and force a non-reproducible rebuild
  every apply. So the rebuild decision *and* the tag must both key on
  `context_blob`, exactly as D3 makes the pulled-image digest sticky.)
- **Tag ordering and version are load-bearing.** A `.build` supports multiple
  `ImageTag=` entries only since **podman v5.3.0** (#23810); on older podman only
  the last is honored and the scheme silently breaks. When a container references
  the build by filename (`Image=web.build`, exactly how `#BuildSelf` resolves),
  podman uses the **first** `ImageTag`, so the content-tag must be listed **first**,
  with a human-friendly tag secondary.
- **Labels** via the `.build`'s native `Label=`: `crei.managed=true`,
  `crei.unit=<name>`, content hash. These only help *filtered* third-party pruning
  (section 8); they protect nothing on their own.
- **Rebuild only on** a `context_blob` delta, `--rebuild`, or first deploy;
  otherwise carry the prior `image_id`/content-tag forward (no rebuild).
- **On rollback,** crei does not start the build service (it would rebuild); it
  restores the prior `.container` (pinned to the prior content-tag) and starts the
  container against the preserved image, after confirming `podman image exists`
  (section 8).

(crei's *own* default `ImageTag` today is `quadlets.localhost/<stem>:latest`, a
crei schema default in `creidhne/build.cue`, not a podman default; podman requires
`ImageTag` and would prefix a bare name with `localhost/`. The content-tag scheme
replaces this default for managed builds.)

**Build auto-update is excluded in v1.** "Update" means "rebuild," and there is no
cheap honest signal for a *meaningful* update: a `FROM`-digest trigger misses the
common case (the fix is usually in `apt`/`pip`/fetched tarballs, which do not move
the base digest), and rebuild-always + dedup-by-result does not help because
non-deterministic builds change the result digest every time regardless. So v1
ships pulled-image auto-update only; builds update via explicit `crei apply` /
`--rebuild`.

## 8. Image retention, labeling, pruning

Protection model, precisely:
- A **tag** keeps an image out of a default `podman image prune` (dangling only),
  but **not** out of `podman image prune -a` (every image not in use by a
  container, tagged or not).
- A **label** protects nothing alone; it only enables *selective* pruning with
  `--filter`, and only built images can be labeled (you cannot label a pulled image).
- The only **durable** protections: in use by a container, crei's own
  manifest-driven `crei prune` (symmetric; it knows both built and pulled digests),
  or a registry backstop.

So pruning is effectively **symmetric**; the real asymmetry is **recoverability
after local loss**:
- **Pulled:** *may* be re-pullable by digest, but registry-dependent. Docker
  Distribution and GHCR keep untagged manifests until a manual/opt-in cleanup
  (Distribution `garbage-collect --delete-untagged`; a GHCR retention Action), so
  the old index digest often persists indefinitely; zot and managed registries
  (Harbor) can GC untagged manifests on a schedule. So re-pull may succeed or fail
  depending on the registry and its config.
- **Built:** gone unless preserved locally or pushed.

**Preserved built image can still be reaped before a later rollback.** After an
upgrade the prior generation's container is replaced, so its image backs no
container and `prune -a` removes it despite the content-tag (the doc states this in
sections 5/7; it is expected, not a bug). The gap is *detection*: probe `podman
image exists` at **rollback time** (mandatory; list-time is a non-authoritative
nicety due to TOCTOU), refuse rollback or require `--force`, and **warn at
generation-write time** when a built image is local-only. Surviving `prune -a` for
a local-only image is impossible, so detection + the registry-push option is the
honest mitigation.

**Optional registry home for built images.** A build can opt into a remote backup
(e.g. `zot.lugoues.dev/quadlets/web`); crei `podman push`es after the build
(prepare phase, no Quadlet push directive exists) and records the returned digest.
Builds then gain the same best-effort registry backstop as pulled images. Push by
immutable digest; reuse `containers-auth.json`; a push failure warns (the local
image still works). Opt-in per build, later phase.

## 9. The health gate (the genuinely hard part)

The riskiest surface; ships opt-in (section 14).

- **What to check.** Default to sweeping the **entire managed set** (downstream B
  failing because A updated is the point), using the declared graph
  (`#self`/`After=`/pods/networks) for *ordering* only. The graph cannot prove the
  absence of a runtime edge ("B calls A over the network"), so it orders the sweep
  but does not bound it.
- **Stabilize, do not sample.** Restarting A makes B briefly see
  connection-refused; a single sample trips on flaps. Require **stable** readiness
  (ready for N consecutive checks / T seconds). `minReadySeconds` is the k8s
  precedent (section 15).
- **Readiness, three distinct tiers (precedence high to low):**
  1. `Notify=true` — the app emits `READY=1` (window bounded by `TimeoutStartSec`).
  2. `Notify=healthy` or a defined `HealthCmd` — readiness tied to the healthcheck
     passing (window bounded by `HealthStartPeriod` + interval*retries). Distinct
     mechanism from `Notify=true`, different window; do not conflate.
  3. default `sdnotify=conmon` / active+settle — **untrusted**: conmon reports ready
     when the process starts, a false-green that lets a bad image *pass* the gate.
     A unit with no real readiness signal must not be silently trusted under
     auto-update; crei warns, and may refuse to auto-update it.
- **Compare against the pre-apply baseline**, so an already-broken service does not
  trip a rollback of an unrelated good change.

**Restart is an action, not a check: `--restart` / `--no-health-gate`.** Because
`daemon-reload` does not restart running units, a default T1+T2 apply that bumps a
digest re-materializes the unit but leaves the **old** container running. Provide a
mode that restarts affected units in dependency order **without** the stabilization
gate, so a common deploy actually takes effect. It is not a new validation "tier";
it must not be the default, and it must warn that it carries **no rollback
guarantee** (an unvalidated restart can leave a broken workload running).

**Companion command `crei graph`.** Renders the declared dependency graph (edges:
`After`/`Requires`, pod/network membership, build-consumes), `--format=dot|json`.
Best available blast-radius view, with the honest caveat that it shows *declared*
coupling only. Single source of truth: the graph crei already computes for ordering.

## 10. Kind matrix

The managed set is eight kinds; they are not uniform. All are snapshot and rolled
back as **opaque bytes** (the section-5 `files` map already does this; no per-kind
mechanism needed). Beyond that:

| Kind | Image pin (D3) | Health gate |
|---|---|---|
| `.container` | yes (`Image=` index digest) | yes (service-producing) |
| `.image` | yes (first-class pulled image) | n/a (oneshot pull) |
| `.build` | yes (`ImageTag` content-tag) | n/a (oneshot build) |
| `.pod` | no | yes (service-producing) |
| `.kube` | **deferred v1** (image lives in the referenced YAML, not pinnable via a quadlet field) | yes |
| `.volume` | **deferred v1** (`Driver=image` carries a pulled ref; noted, not pinned in v1) | n/a |
| `.network` | no | n/a |
| `.artifact` | no | n/a |

Oneshot kinds have no readiness signal and would fall into the distrusted
active+settle tier; do not invent per-kind health probes for them, T2
generator-output validation covers them at the right altitude. `.kube` and
`.volume Driver=image` image identity are explicitly out of v1 scope, with the
rationale recorded so it is a decision, not an omission.

## 11. Limits of rollback: stateful services

**Rollback restores the file + image layer, not data.** If a new image ran a
forward, non-backward-compatible migration against a persistent volume, rolling the
image back leaves the prior binary pointed at a migrated store it may corrupt. The
services operators most want protected (databases) are exactly where automatic
image rollback is **unsafe**.

- **Separate detect from remediate.** The gate still *detects* failure. The action:
  - stateless unit -> roll back (file+image).
  - unit flagged **`no_auto_rollback`** -> **halt and alert**, leaving the
    failed-but-known state for a human.
- **A `no_auto_rollback` unit poisons whole-plan rollback.** Concrete break: a plan
  with a stateless `web` + a `no_auto_rollback` `db`; `db` forward-migrates then
  fails the gate. crei must not revert `db`, but reverting `web` to its old image
  points the OLD schema's binary at a now-migrated DB, strictly worse than leaving
  the new generation up, and a corruption vector. Since the graph cannot prove
  `web` is independent, the rule is: **if any unit in a plan is `no_auto_rollback`,
  the whole plan escalates to halt-and-alert** (leave the new generation up,
  `outcome:"halted"`), it does not partially roll back. This also resolves open
  question 5: **partial rollback is never the silent auto-default.**
- **"Halt" is concrete:** stop driving the transition, leave the new generation's
  files+units in place, mark the generation `halted`, emit a loud sticky non-zero
  signal, require human action. (This is the third manifest outcome the binary
  success/rollback model must carry.)
- **Stateful services should generally not be auto-updated at all.**

## 12. Unattended auto-update safety: oscillation, quarantine, backoff

A rollback is **not** a success. If the timer pulls digest X, the gate sees B flap,
it rolls back and reports "ok," the next tick pulls X again: a machine that flaps a
service every interval and reports green.

- A rollback (or halt) is a **loud, non-zero, sticky** signal: exit code, log,
  optional notification hook. Never plain success.
- **Quarantine the failed target**, keyed on the resolved digest, in the state dir:
  record "digest X failed the gate for unit Y at generation Z." Auto-update must
  **not re-attempt X** until the authored ref resolves to a *different* digest
  (upstream moved again, the auto-clear branch) or a human clears it. Provide
  `crei quarantine list` / `crei quarantine clear` (exact granularity parked in
  open questions, like `crei prune`).
- **Backoff** so even distinct repeated failures do not hammer.

On the k8s analogy, corrected: k8s does not auto-rollback, but the mechanism is
*not* "halt because incremental." When a rollout exceeds `progressDeadlineSeconds`
(default 600s) the controller only sets a `ProgressDeadlineExceeded` condition and
otherwise keeps retrying toward the desired state, taking no remediation
(automatic rollback is documented as "not yet implemented," delegated to a higher
orchestrator or manual `kubectl rollout undo`); because rollouts are incremental it
*leaves* a mixed old+new state. crei's transaction is **atomic per service set**,
so for crei rolling back to the last-good generation and **quarantining** the bad
digest is the right unattended behavior (you end up on the good image, not stuck on
the bad one). The transferable lesson is the quarantine/backoff, not the
incremental-rollout story. Greenboot's `boot_counter` (section 15) is *bounded
retry of the same deployment*, not a content-keyed quarantine, so crei's
per-resolved-digest quarantine is a genuine contribution over both.

This whole surface ships **last, experimental, pulled-images-only.**

## 13. Crash safety and concurrency

- **Concurrency:** the transaction lock is **per systemd scope** (user session or
  system manager), not per quadlet dir, because `daemon-reload` is scope-global, so
  two applies to different dirs in one scope still race on reload. Use **`flock(2)`
  on an open fd** (auto-released on process death, which a Go binary gets via the
  default close-on-exec; one portability note: do not leak the fd to a child).
  flock is a **same-host** tool only; cross-host protection is the `host` sentinel
  (D1), since NFS lock semantics are unreliable.
- **Crash safety:** writing the new generation and flipping `current` atomically
  (rename) gets us most of the way. Full mid-apply crash recovery (detect an
  incomplete journal on next run) is phase 2 and shares machinery with the
  FAILED-DIRTY recovery path (`crei recover`). Once unattended auto-update ships,
  crash safety stops being optional.

## 14. Phasing

1. **Manual rollback core (shippable product).** Sidecar store + transactional
   file apply (T1+T2) + `crei rollback` + `crei generations` + gen-0 baseline +
   image pinning (`.container`/`.image`/`.build` via `podman manifest inspect`
   index digest) + sticky pins (pulled digest, build content-tag) + the kind matrix
   + host sentinel + state-dir backup/warn guidance. The NixOS/OSTree generations
   model for quadlets; no health/auto-update risk.
2. **Crash safety (journal replay) + per-scope flock + the FAILED-DIRTY /
   `crei recover` groundwork.** Prerequisite for anything unattended.
3. **T3 health-gated auto-rollback for manual `apply` (opt-in, bounded).**
   Three-tier readiness + stabilization, whole-set check / graph ordering, the
   stateful `no_auto_rollback` detect-and-halt + whole-plan escalation, the
   terminal-failure path (`crei status`/`recover`), and the `--restart` /
   `--no-health-gate` mode.
4. **`crei auto-update` + timer (experimental, pulled-images-only).** Quarantine
   (`list`/`clear`) + backoff + loud alerting first-class; pull-free index-digest
   resolution on the timer path; builds excluded.

De-pin/hand-back is a CUE-intent render rule (D6), not a phased command.

## 15. Prior art (vocabulary and validation, not a model to conform to)

crei's hard parts (digest-pin ownership, sticky resolution, build
non-reproducibility, per-digest quarantine) are already solved here and in places
exceed these frameworks; cite them for legibility, not adoption.

- **greenboot** (FCOS/bootc): `required.d` (gating checks whose failure rolls back)
  vs `wanted.d` (advisory) maps to crei's gate-vs-advisory and motivates
  `no_auto_rollback`; `redboot`/`red.d` validates the loud-sticky-terminal-state
  model. Divergence: greenboot's `boot_counter` is bounded retry of one deployment,
  not a content-keyed quarantine (section 12).
- **Kubernetes:** `revisionHistoryLimit=10` anchors the retention-N default;
  `minReadySeconds` (a per-pod stay-ready dwell, default 0), **not**
  `progressDeadlineSeconds` (a rollout failure deadline, default 600s), is the
  stay-ready-window precedent for the section-9 stabilization window.
- **podman `policy.json`/sigstore** is the future *provenance* layer; digest-pinning
  is integrity, not provenance. Out of scope now, noted as the natural extension.

## 16. Open questions

Resolved this pass: gating flag (crei-namespaced, not `AutoUpdate=`); build
auto-update (excluded v1); health scope (whole set, graph ordering, stabilization);
readiness (three tiers, active+settle untrusted); rollback granularity (whole-plan
default, never silent-partial; `--rollback-scope=unit` later); retention
(keep-last-N, `unlimited` keyword, higher floor for builds); image resolution
(index digest via `podman manifest inspect`, no skopeo hard dep); de-pin (CUE-intent
render rule, no `crei unmanage`); deployment-id (path-stable + host sentinel).

Still open:
- Concrete defaults: stabilization window length, retention N, backoff schedule.
- Refuse vs warn for a unit with no real readiness signal under auto-update.
- `crei prune` and `crei quarantine clear` UX/granularity; whether `crei prune`
  runs after a successful apply or only on demand.
- gen-0 orphan-managed-file disposition policy on first apply.
