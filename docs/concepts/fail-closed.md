# Fail-closed

argo-guard is a security gate, so its behavior under error is part of the contract: **when in doubt, fail the sync — never emit unchecked manifests.**

## Exit-code contract

`argo-guard generate` returns one of three codes. Manifests are written to **stdout** only on success; reports go to **stderr**.

| Exit | Meaning | Manifests emitted? | Argo result |
|---|---|---|---|
| `0` | Pass (clean, or warnings only) | ✅ stdout | Sync proceeds |
| `1` | Policy violation (one or more `deny`) | ❌ | Sync fails; report in UI |
| `2` | Internal / fail-closed error | ❌ | Sync fails; error in UI |

See the [Exit codes reference](../reference/exit-codes.md) for the per-condition table.

## What fails closed

| Condition | Result | Why |
|---|---|---|
| `kustomize build` fails | exit 2 | Broken manifests shouldn't deploy anyway |
| Broken Rego / conftest execution error | exit 2 | A broken *policy* must not silently pass traffic through |
| `guard.yaml` malformed | exit 2 | Can't trust bundle selection |
| No bundle matched at all | exit 2 (clear message) | Never run with zero enforcement |
| Policy repo unreachable on **cold start** (no cache) | exit 2 | Never evaluate with no policies loaded |
| Policy violation found | exit 1 | The normal "deny" path |

## The one deliberate availability trade-off

If the policy repo is unreachable but a **previous cache exists**, argo-guard serves the **last-known-good** policies and logs a stale-cache warning (it does *not* fail). This is intentional: a transient Git outage must not freeze *every* sync across the platform. The cost is that a newly *tightened* rule is delayed until the next successful fetch. See [Caching](../operations/caching.md).

!!! note "Fail-closed is fixed behavior"
    Fail-closed is not a tunable knob — there is no "fail-open" mode. The only softness is the stale-cache window above, and it still serves the most recent policies you successfully fetched.

## Soft-launching a rule without blocking

Rego `warn` results are surfaced in the report but **do not** change the exit code. Ship a new rule as `warn` first, watch what it would have blocked, then promote it to `deny`. See [Writing Rego](../policies/writing-rego.md).
