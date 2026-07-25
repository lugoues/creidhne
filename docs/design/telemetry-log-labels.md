# Telemetry log labels

Status: shipped.

## Problem

journald exposes some container metadata as fields (`CONTAINER_NAME`,
`IMAGE_NAME`, ...), but not in the shape telemetry wants:

- `CONTAINER_NAME` is podman's name, `systemd-<unit>` for a quadlet
  container — prefixed and not the clean unit identity.
- There is no field for the owning quadlet, nor the pinned image digest
  (the immutable identity you actually want to correlate on).

Extracting this downstream (parsing the systemd- prefix, joining to a
registry) is brittle. crei already knows all of it at render time.

## Mechanism

podman's journald driver supports `--log-opt label="KEY=VALUE"`
(repeatable, journald-only; Quadlet `LogOpt=label=…`). crei stamps a fixed
set onto every container:

- `QUADLET` — the quadlet name.
- `QUADLET_UNIT_NAME` — the crei stem (`<quadlet>` or `<quadlet>-<name>`),
  the clean unit identity behind journald's `systemd-`-prefixed name.
- `IMAGE` / `IMAGE_DIGEST` — the resolved image ref, split on `@`. The
  digest is present only when the image is pinned (a registry `#ref`, or a
  hand-written `repo:tag@sha256:…`).

Because the label opt is journald-only, `#Container.LogDriver` now defaults
to `journald` (`*"journald" | #LogDriver`). Override it to opt out — the
labels only render under journald (a template guard), so a container on
another driver carries neither the forced driver nor dead labels.

## Why pure CUE (not Go)

Unlike the build-content-hash (which needs a sha256 CUE can't compute),
these labels are string interpolation of values the schema already holds:
`_stem`, the quadlet name (`_qn`, threaded into each container's `_quadlet`
by `#Units`), and `imageString`. So it lives in `container.cue` +
`container.tpl`, like any other rendered field — visible in the schema, and
the golden harness renders (and tests) it directly, rather than a Go
injection whose output the fixtures wouldn't see.

## Runtime requirement

Emitting a custom `--log-opt label` field into journald needs podman >= 6.0.2
(podman#26203) *and* a conmon with matching support (conmon#562, "add container
labels to log entries on journald"). conmon writes the container's log lines, so
the field only appears once conmon knows to emit it. On older stacks the option
is still valid — it lands in the `.container` file and shows in `podman inspect`
— but conmon never writes the field, so `journalctl QUADLET=…` returns nothing.
The field name is emitted verbatim (no prefix), so `journalctl QUADLET=bookorbit`
works once the runtime is new enough. The directive is harmless on older podman,
so crei emits it unconditionally rather than gating on a runtime probe.

## Notes / limits

- A container referencing a `.build` image gets `IMAGE=<the reference>`
  (the tag or `<stem>.build` it names); there is no registry digest for a
  locally-built image at render time.
- The value passes through verbatim; podman's `--log-opt label` also
  accepts `{{.Foo}}` inspect-format keys, but crei injects resolved
  literals so the digest is the one crei pinned, not whatever runs.
