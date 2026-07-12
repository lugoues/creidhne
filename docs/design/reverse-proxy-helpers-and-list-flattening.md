# Reverse-proxy pair networks, helper ecosystem, and list flattening

Status: list flattening in progress; helpers and extras repo planned.

## Problem

The common traefik-on-podman pattern gives every exposed service a dedicated
"pair" network shared only with the proxy, because podman networks have no
peer isolation (all peers on a network see each other). The pattern is
correct but footgun-rich:

1. Boilerplate drift: each exposure needs a network unit + service attachment
   + proxy attachment + 3-4 labels.
2. Forgot to attach the proxy to the new network: 502.
3. Wrong/missing `traefik.docker.network`: the container sits on several
   networks (pair + db), traefik picks an arbitrary IP, intermittent 502s.
   The label wants the runtime network name, which people hand-type wrong.
4. The silent isolation leak: a third container attaches to a pair network.
   Nothing fails; the isolation is simply gone.
5. Rule/port typos in the traefik label DSL.

## Design: `#Exposes` (value-computation helper, no magic)

Rejected: a registry + mixin + aggregation design (auto-attaching traefik via
a package-level `exposures` struct). It solved footgun 2 but front-loaded
complexity and indirection; boilerplate is the least dangerous problem.

Adopted direction (the `#JSONLabel` treatment): a helper that only computes
values; every placement stays explicit in the user's quadlets.

```cue
grafana: creidhne.#Quadlet & creidhne.#Exposes & {
    name: "grafana"
    #exposes: {
        port: 3000
        rule: "Host(`grafana.example.lan`)"
    }
    units: {
        networks: proxy: #exposes.#network        // key is a pure handle
        #container: Container: {
            Image:   "docker.io/grafana/grafana:11"
            Network: [units.networks.proxy.#self]
            Label:   [#exposes.#labels]           // nested; see list flattening
        }
    }
}

traefik: creidhne.#Quadlet & {
    name: "traefik"
    units: #container: Container: {
        Network: [grafana.units.networks.proxy.#self]  // one line per service
    }
}
```

Key decisions (settled):

- **Quadlet-level mixin**: `#Quadlet & #Exposes & {#exposes: {...}}`. The
  `#exposes` config is a definition field (closedness-exempt), and because
  the mixin unifies into the quadlet it reads siblings (`name`) directly.
- **The helper owns the runtime network name.** `#network` sets both
  `name: "traefik-proxy"` (file: `<quadlet>-traefik-proxy.network`) and
  `NetworkName: "<quadlet>-traefik-proxy"` (runtime). Since `NetworkName` is
  schema input, not a derived value, this duplicates no stem/name formula;
  `#labels`' `traefik.docker.network` uses the same string. The map key
  (`proxy:`) is a pure CUE handle. Attachments always go through the
  canonical `units.networks.<key>.#self`.
- **Opinionated pair-network defaults** (override-friendly, star-defaulted):
  `DisableDNS: *true | bool` (traefik dials container IPs from inspect data,
  DNS is dead weight) and `Options: ["no_default_route=true"]` (pair network
  must never become an egress path). Verify exact netavark option spelling at
  implementation. Also a `creidhne.pair=<name>` marker label for the future
  lint rule.
- **Traefik attaches, it never defines**: exactly one owner per pair network
  (the service quadlet). The proxy side is a `Network:` entry.
- **Naming/layering**: generic `#ReverseProxySpec` (network pattern) +
  `#TraefikProxySpec: #ReverseProxySpec & {...labels...}` on top.

Footguns 1/3/5 die in the helper. Footguns 2/4 are closed-world cardinality
checks, inexpressible in CUE (unification cannot assert "nothing else
references this"); they belong to a later `crei lint` rule keyed on the
marker label: a pair network must have exactly 2 attachments in the graph
(<2 = proxy never joined; >2 = isolation leak).

## Extras repo (`creidhne-extras`)

Core schema stays data + validators. Opinionated app-shaped helpers
(`#TraefikProxySpec`, `#BMSpec`, `DocktailSpec`, ...) move to a separate CUE
module. Blocker to solve first: crei's offline story only covers the
embedded `creidhne@v0` (binary embeds it, vendors to `cue.mod/usr/`). A
second module needs a vendoring mechanism. Options:

1. `crei vendor <module>`: fetch a git-hosted CUE module into
   `cue.mod/usr/`; offline after vendor. (Preferred; decide generic vs
   extras-only.)
2. Embed extras in the binary: rejected, ties extras to core release cadence.
3. Copy-in snippets: zero infra, no versioning.

## List flattening (core feature, prerequisite)

`Label: [#exposes.#labels]` nests a list in a list. Helpers compose better
when every list property accepts one level of nesting and crei flattens it.

Mechanism: one flatten semantic (splice one level), applied at the two
pipeline phases that consume lists, plus type permission:

```
user CUE -> CUE validation -> CUE comprehensions -> Go decode -> templates/state/graph
              needs loosened     needs per-element     needs the Go flatten
              types (all 264)    dispatch (14)         pass (the rest)
```

- **Schema loosening (permission, not flattening)**: every list field's
  element type `T` becomes `(T | [...T])`. Validation *is* CUE evaluation;
  there is no pre-validation Go hook, so this cannot be substituted.
- **CUE-side per-element dispatch** in the 14 `xStrings` comprehensions
  whose consumers run in CUE (container volume/mount/network/tmpfs/label/
  secretStrings; pod volume/network/labelStrings; build network/volume/
  labelStrings; volume and network labelStrings). Implementation note:
  `list.FlattenN` was the obvious tool but breaks on elements typed with the
  loosened `(T | [...T])` disjunction in cue v0.16 (the comprehension binding
  is lost for scalar elements; nested elements flatten fine — the failure is
  the unresolved scalar-or-list disjunction, not the nesting). Each
  comprehension instead branches per element: scalar renders directly, a
  one-level list gets an inner loop over `(e & [...])`.
- **Go-side universal pass** in `eval`'s decode walk (alongside the
  json.Number coercion): any `[]any` element that is itself `[]any` splices,
  one level, everywhere. Covers direct-template fields and the 160 generated
  section lists; makes state/graph/importer data flat too.

Census (main, 2026-07-12): 86 hand-written unit-section list fields
(Container 28, Pod 14, Build 14, Network 9, Kube 9, Volume 6, Image 3,
Artifact 3) plus two the first sweep missed: Build.ImageTag (defaulted
disjunction shape) and unit_deps.cue's 16 #ServiceName overlay fields (they
tighten the generated deps, so they must loosen in lockstep or After= etc.
reject nesting). 160 generated section lists (one generator edit), 14 CUE
comprehensions, 7 helper-internal option lists (`#self` decorations,
`#TmpfsSpec.options`, ...) deliberately out of scope: they are value DSLs,
not composition points.

Depth is exactly 1, documented: helpers emit flat lists; deeper nesting is a
smell and stays a type error.

Oddities: `Kube.Yaml` carries a non-empty constraint (compose the loosening
with `[_, ...]`); `Build.Context` is a pattern-keyed map, not a list;
`Network.Label` never got the `#LabelValue`/`labelStrings` treatment (only
unit whose labels can't use `#JSONLabel`), folded into this change.

## Sequencing

1. Core: list flattening (this change).
2. Core: `crei vendor` (or chosen alternative).
3. `creidhne-extras` bootstrap: module layout, `#ReverseProxySpec`/
   `#TraefikProxySpec`, migrate app specs as they accrete.
4. Optional: the exactly-2-attachments lint rule on the pair marker.
