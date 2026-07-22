# Example argo-guard policy repo

Layout mirrors what `GUARD_POLICY_REPO` should contain:

- `guard.yaml` — bundle registry with match/exclude.
- `global/` — a bundle (`match: {}`, always applies): Rego + `data.json`.
- `production-transitions/` — change-aware rules evaluated against the last
  successful sync only for Applications in the `production` project.

The trust context is available in Rego as `data.context` (`data.context.repo`,
`.project`, `.namespace`, `.appLabels`). External allowlists live in `data.json`.

Test policies locally:
`conftest verify --policy global/ --data global/`

Transition tests:
`conftest verify --policy production-transitions/`
