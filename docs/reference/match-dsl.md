# Match DSL grammar

The selection language used in [`guard.yaml`](../policies/guard-yaml.md)'s
`match` and `exclude` blocks. It routes bundles to apps based on the trust
context; it does **not** express resource policy (that's Rego).

## Grammar (informal)

```
Expr        := { Field* , Logical* }          # all entries AND together

Field       := repo:      Condition
             | project:   Condition
             | namespace: Condition
             | label:      { <key>: Condition , ... }

Logical     := and: [ Expr , ... ]            # all sub-exprs true
             | or:  [ Expr , ... ]            # at least one sub-expr true

Condition   := <scalar>                        # shorthand for { equals: [<scalar>] }
             | { Operator* }                   # all present operators AND together

Operator    := equals:        <scalar> | [<scalar>...]   # list = OR / "in"
             | notEquals:     <scalar> | [<scalar>...]
             | like:          <glob>
             | notLike:       <glob>
             | startsWith:    <string>
             | notStartsWith: <string>
             | endsWith:      <string>
             | notEndsWith:   <string>
```

## Semantics

- **Empty `Expr` (`match: {}`) matches everything** — the global baseline.
- Within one `Expr`, all fields and logical nodes are **AND**ed.
- Within one `Condition`, all present operators are **AND**ed.
- `equals` / `notEquals` accept a scalar or a list; a **list is OR** (membership).
- `like` / `notLike` use glob wildcards: `*` matches any run of characters
  **including `/`**, `?` matches one character.
- A bundle applies **iff** `eval(match)` is true **AND** `eval(exclude)` is
  false. Absent `exclude` ⇒ never excluded.

## Fields and their sources

| Field | Trust-context value |
|---|---|
| `repo` | `ARGOCD_APP_SOURCE_REPO_URL` |
| `project` | `ARGOCD_APP_PROJECT_NAME` |
| `namespace` | `ARGOCD_APP_NAMESPACE` |
| `label.<key>` | `ARGOCD_ENV_<KEY>` (lowercased key) |

## Out of scope (by design)

- No numeric/arithmetic operators (use Rego for replica caps, etc.).
- No regex (`like` covers wildcards).
- Operates on single string fields only.

## Example

```yaml
match:
  and:
    - repo: { startsWith: "https://git.corp/infra/" }
    - or:
        - namespace: { like: "platform-*" }
        - label: { tier: { equals: [frontend, edge] } }
exclude:
  namespace: { equals: [kube-system, argocd] }
```

Applies to apps from an infra repo whose namespace looks like `platform-*` **or**
whose `tier` label is `frontend`/`edge` — except in `kube-system`/`argocd`.
