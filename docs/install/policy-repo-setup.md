# Policy repo setup

Policies live in their own Git repository — versioned and PR-reviewed like any
GitOps content, with no ConfigMap size limit and no image rebuild to change a
rule.

## Layout

```
argo-guard-policies/
├── guard.yaml                 # bundle registry: dir + match/exclude
├── global/                    # a bundle (match: {}, always applies)
│   ├── restrictions.rego
│   ├── restrictions_test.rego
│   └── data.json              # allowlists, e.g. trustedRepos
├── namespaces/
│   └── payments/ ...          # match: namespace = payments
└── projects/
    └── platform/ ...          # match: project = platform
```

The `guard.yaml` at the repo root maps each bundle directory to its match rules
— see [guard.yaml & match DSL](../policies/guard-yaml.md).

## Point the sidecar at it

Set these on the sidecar (see [Deploy](deploy.md)):

| Variable | Example | Notes |
|---|---|---|
| `GUARD_POLICY_REPO` | `https://git.corp/platform/argo-guard-policies.git` | The clone URL |
| `GUARD_POLICY_REF` | `main` | Pin to a branch or a tag |
| `GUARD_POLICY_TTL` | `60s` | Cache refresh interval — see [Caching](../operations/caching.md) |

!!! tip "Pin to a tag for change control"
    Pointing `GUARD_POLICY_REF` at a tag (e.g. `policies-2026-06`) means policy
    changes only take effect when you move the tag — useful if you want an
    explicit promotion step rather than "merges to main go live in `TTL`."

## Recommended repo hygiene

- **Require PR review** on the policy repo — this is your real change-control
  and break-glass surface ([Break-glass](../operations/break-glass.md)).
- **Run policy tests in the policy repo's own CI** with `conftest verify` so a
  rule that breaks its own tests can't merge. See [Testing policies](../policies/testing.md).
- **Pin the same conftest version** your image uses, in that CI, to avoid
  Rego-dialect drift.
