# Build content hash (k8s-style version stamp)

Status: shipped.

## Problem

crei's change detection keys on unit-file content: a unit is stale when
its running process predates the last content change to *its own file*. A
`.build` file holds only pointers (`File=images/<stem>.Containerfile`,
`SetWorkingDirectory=…`), not the Containerfile or context bytes — those
live in separate `images/` artifact files that have no service. So:

- Changing a Containerfile/context did not flag the `.build` unit stale
  (its file was unchanged). *Under*-detection.
- A container running a just-rebuilt image was never flagged: its
  `.container` file is unchanged, and crei tracks no image identity.

A first fix folded the `images/` artifacts' timestamps into the build
unit's staleness (`newestBuildArtifact`), but it was a special case and
couldn't reach the consuming container.

## Mechanism

Borrow k8s's pod-template-hash: fold the build's inputs into a version and
let it ride the unit file. `eval.injectBuildHashes` (run once in
`LoadAndValidate`, over the whole project) hashes each build's data and
stamps `Annotation=creidhne.build-hash=<12hex>` onto:

- the **build** unit — so any input change alters the `.build` file, and
  the normal per-file mechanism flags it. The hash covers the entire
  build data (Containerfile, context, BuildArg, ImageTag), so a base-image
  or arg change moves it too.
- every **container** whose `Image=` resolves to that build — so a rebuild
  changes the `.container` file and flags the consumer, with no runtime
  image-digest inspection.

The stamp is an OCI annotation, so it is also visible on the built image
and the running container (`podman inspect`).

## Why LoadAndValidate

The injection must be identical across every render path — full render
(apply/plan/diff), per-quadlet render (state recording), and the golden
harness — or a consumer's file would differ between apply and record and
oscillate add<->change. `LoadAndValidate` is the single chokepoint all of
them pass through; injecting there mutates the shared `UnitRecord`s once,
so cross-quadlet image references resolve and every render subset sees the
same stamped data.

## Applying a rebuild

A Containerfile change flags the build *and* its consumer together (both
files move). `restart --stale` collects both services into one
`systemctl restart` call; quadlet emits `Requires=`+`After=<stem>-build`
on the consumer, so systemd's transaction runs the build oneshot (which
rebuilds) before recreating the container against the fresh image. A
failed rebuild blocks the container restart (`Requires=`), which is the
safe outcome. No crei-side ordering code is needed.

## Notes / limits

- The hash covers build *inputs*, not output bits: a moving base
  (`FROM …:latest`) that produces different bits from an identical
  definition is invisible. Digest-pin bases for true reproducibility.
- `staleNote` hides the build-hash annotation from the changed-keys diff
  and translates a moved hash into a human reason: `Containerfile/context`
  on a build (when nothing else changed), `image rebuilt` on a consumer.
