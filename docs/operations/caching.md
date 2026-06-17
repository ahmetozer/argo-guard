# Caching

The policy Git repo is cached locally in the sidecar so argo-guard doesn't clone
on every generation. The cache balances **freshness** (new rules take effect
quickly) against **availability** (a Git outage must not freeze all syncs).

## How it works

- **Cold start** (no cache yet): `git clone` the repo at `GUARD_POLICY_REF`. If
  the clone fails, argo-guard **fails closed** (exit 2) — it never evaluates with
  no policies.
- **Fresh** (cache younger than `GUARD_POLICY_TTL`): use it as-is, no network.
- **Stale** (older than the TTL): `git fetch` + checkout the ref. On success the
  cache is refreshed.
- **Stale + fetch fails**: serve the **last-known-good** cache and log a
  stale-cache warning. The sync is **not** failed.

```
                         ┌─ cache exists? ─┐
                  no ────┤                 ├──── yes
                  │      └─────────────────┘      │
            git clone                       younger than TTL?
            success? ──no──▶ FAIL CLOSED      │         │
                  │                          yes        no
                 use                          │         │
                                            use     git fetch
                                                   success? ──no──▶ serve stale + warn
                                                       │
                                                      use (refreshed)
```

## Tuning `GUARD_POLICY_TTL`

| TTL | Effect |
|---|---|
| Short (e.g. `30s`) | Rules propagate fast; more `git fetch` traffic |
| Long (e.g. `10m`) | Less traffic; tightened rules take longer to take effect |

`60s` is a sensible default. Remember the **stale window**: when the policy repo
is unreachable, a *newly tightened* rule is delayed until the next successful
fetch. This is the deliberate availability trade-off described in
[Fail-closed](../concepts/fail-closed.md).

## Cache location

The cache lives at `GUARD_POLICY_CACHE` (default
`/var/cache/argo-guard/policies`), inside the `policy-cache` volume mounted in
the sidecar. It's an `emptyDir` by default, so it's rebuilt (cold start) on pod
restart — the first generation after a restart performs a clone.

## Local / no-repo mode

If `GUARD_POLICY_REPO` is empty, argo-guard treats an existing
`GUARD_POLICY_CACHE` directory as the policy root and skips Git entirely
(failing closed if the directory is absent). This is used by the end-to-end
tests and for local experimentation; in production, always set
`GUARD_POLICY_REPO`.
