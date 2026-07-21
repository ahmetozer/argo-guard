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

## ConfigMap delivery (no git)

With `GUARD_POLICY_REPO` empty and `GUARD_POLICY_FLAT=true`, policies arrive
as a ConfigMap synced by an Argo CD Application and mounted into the sidecar —
argo-guard never runs git and needs no credentials. See
[Policy repo setup](../install/policy-repo-setup.md#zero-credential-delivery-via-configmap).

Freshness in this mode is **Argo CD sync + kubelet propagation**: after the
policy Application syncs, kubelet refreshes the mounted ConfigMap within about
a minute (the mount must not use `subPath`, which would freeze it). Nothing on
this page besides this section applies: there is no clone, no TTL, and no
stale-cache window — the mounted ConfigMap is always authoritative, and a
missing/empty mount fails closed.

## Local / no-repo mode

If `GUARD_POLICY_REPO` is empty (and `GUARD_POLICY_FLAT` is unset), argo-guard
treats an existing `GUARD_POLICY_CACHE` directory as the policy root and skips
Git entirely (failing closed if the directory is absent). This is used by the
end-to-end tests and for local experimentation; in production, use git
delivery (`GUARD_POLICY_REPO`) or ConfigMap delivery (`GUARD_POLICY_FLAT`).
