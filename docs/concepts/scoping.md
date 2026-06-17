# Scoping

Different teams need different rules: a platform repo legitimately creates `Namespace`s and `ClusterRole`s; a product team's app should not. argo-guard supports **layered, composable scoping** so one app can be subject to several rule sets at once.

## How bundles compose

Each bundle in [`guard.yaml`](../policies/guard-yaml.md) declares a `match` block (and optionally an `exclude` block). At generation time argo-guard evaluates *all* bundles against the [trust context](trust-model.md):

> A bundle applies **iff** `eval(match)` is true **AND** `eval(exclude)` is false.

**Every matching bundle applies, and the violations are the union.** More matches → stricter, never looser. A bundle with `match: {}` always applies — it is your global baseline.

```
app: repo=https://git.corp/infra/p.git, project=platform, namespace=payments
        │
        ├─ global            (match: {})                       ✅ always
        ├─ projects/infra    (match: repo startsWith infra/)   ✅
        ├─ namespaces/payments (match: namespace = payments)   ✅
        └─ projects/team-a   (match: project = team-a)         ✗ skip
        ⇒ rules enforced = global ∪ projects/infra ∪ namespaces/payments
```

## The match dimensions

| Dimension | Field | Typical use |
|---|---|---|
| Global | `match: {}` | Baseline every app gets (no privileged, limits required, registry allowlist) |
| Repo | `repo` | Trust anchor — privileged infra repos. See [Trust model](trust-model.md). |
| Project | `project` | Per-team rules keyed on the Argo AppProject |
| Namespace | `namespace` | Environment- or tenant-specific rules |
| Label | `label.<key>` | Convenience routing (author-controlled — not for privilege) |

The matching language (operators, `and`/`or`, `exclude`) is documented in [guard.yaml & match DSL](../policies/guard-yaml.md) and formally in the [Match DSL grammar](../reference/match-dsl.md).

## Selection vs. granting

Selection is **additive** — it can only add restrictions. To *grant* a repo extra freedom, you don't select fewer bundles; you write a rule with an exemption keyed on `data.context` (see [Trusted repos](../policies/trusted-repos.md)). Use `exclude` only to skip an *entire* bundle for some context; use a Rego exemption to relax a *single* rule while keeping the rest.

## Never zero enforcement

If you keep a `match: {}` global bundle (recommended), every app always gets the baseline even when no team/namespace/repo bundle matches. If selection ever resolves to **no** bundles, argo-guard fails closed with a clear error rather than silently allowing everything — so a `guard.yaml` that forgets its global baseline is caught, not waved through.
