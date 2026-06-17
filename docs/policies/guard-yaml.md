# guard.yaml & the match DSL

`guard.yaml` is the registry at the root of your policy repo. It lists each
bundle directory and the conditions under which it applies. It is the only place
the match DSL is used — all *resource* policy lives in Rego.

## Shape

```yaml
bundles:
  - dir: global
    match: {}                      # always applies (baseline)

  - dir: projects/infra
    match:
      and:
        - repo: { startsWith: "https://git.corp/infra/" }
        - or:
            - namespace: { like: "platform-*" }
            - label: { tier: { equals: frontend } }
    exclude:
      namespace: { equals: [kube-system, argocd] }

  - dir: namespaces/payments
    match:
      namespace: payments          # shorthand for { equals: payments }
```

A bundle applies **iff `match` is true AND `exclude` is false**. Every matching
bundle applies; violations are the union ([Scoping](../concepts/scoping.md)).

## Fields

Matching operates only on the [trust context](../concepts/trust-model.md):

| Field | From |
|---|---|
| `repo` | `ARGOCD_APP_SOURCE_REPO_URL` — the trust anchor |
| `project` | `ARGOCD_APP_PROJECT_NAME` |
| `namespace` | `ARGOCD_APP_NAMESPACE` |
| `label.<key>` | `ARGOCD_ENV_*` plugin params (author-controlled) |

## Operators

Each field takes one or more operators; **all present operators must hold**
(AND within a field). An empty condition matches anything.

| Operator | Meaning |
|---|---|
| `equals` | exact; **a list means OR / "in"** — `equals: [a, b]` |
| `notEquals` | negation of `equals` |
| `like` / `notLike` | glob wildcard — `*` (any run, crosses `/`) and `?` |
| `startsWith` / `notStartsWith` | prefix |
| `endsWith` / `notEndsWith` | suffix |

**Shorthand:** `{ repo: "x" }` ≡ `{ repo: { equals: "x" } }`.

## Composition

- **Multiple fields in one block → implicit AND** (multi-dimension matching).
- **`and: [ … ]` / `or: [ … ]`** → explicit logical nodes, nestable.
- **`exclude`** uses the same shape; if the context matches it, the bundle is skipped.

```yaml
match:
  or:
    - project: { equals: [team-a, team-b] }    # team-a OR team-b
    - repo: { startsWith: "https://git.corp/shared/" }
```

## What stays out of the DSL

The match language is deliberately small — string fields only, no
numeric/arithmetic operators, no regex. It exists to **route bundles to apps**.
All resource and field-value policy (allowed kinds, required limits, replica
caps, registries) is expressed in Rego, where the full language is available.
See [Writing Rego](writing-rego.md) and the formal
[Match DSL grammar](../reference/match-dsl.md).
