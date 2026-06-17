# Exit codes

`argo-guard generate` communicates its verdict to Argo CD purely through its exit
code and streams:

- **stdout** — the rendered manifests, written **only** on success (exit 0).
- **stderr** — the violation/warning report and any error messages.

| Exit | Meaning | stdout | Argo outcome |
|---|---|---|---|
| `0` | Pass — clean, or `warn`-only | rendered manifests | Sync proceeds (warnings/stale notices shown but non-blocking) |
| `1` | Policy violation — one or more `deny` | _(empty)_ | Sync fails; report shown in the UI |
| `2` | Internal / fail-closed error | _(empty)_ | Sync fails; error shown in the UI |

## What maps to exit 2 (fail-closed)

| Condition | |
|---|---|
| `kustomize build` failed | broken manifests shouldn't deploy |
| conftest execution error / broken Rego | a broken policy must not silently pass |
| `guard.yaml` missing or malformed | can't trust bundle selection |
| no policy bundle matched | never run with zero enforcement |
| policy repo unreachable on cold start (no cache) | never evaluate with no policies |

A non-zero conftest exit caused by **policy failures** (with valid JSON output)
is **not** an execution error — it is mapped to exit 1 (violation). Only true
execution failures map to exit 2.

See [Fail-closed](../concepts/fail-closed.md) for the rationale.
