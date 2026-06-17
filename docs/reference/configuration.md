# Configuration

argo-guard is configured entirely through environment variables on the sidecar
container. There is no config file.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `GUARD_POLICY_REPO` | _(empty)_ | Clone URL of the policy Git repo. If empty, argo-guard uses an existing `GUARD_POLICY_CACHE` directory as-is (local mode) and never runs Git. |
| `GUARD_POLICY_REF` | `main` | Branch or tag to check out in the policy repo. Pin to a tag for explicit promotion. |
| `GUARD_POLICY_TTL` | `60s` | How long a cached policy checkout is considered fresh before a refresh is attempted. Accepts Go durations (`30s`, `5m`) or a bare integer (seconds). |
| `GUARD_POLICY_CACHE` | `/var/cache/argo-guard/policies` | Local path for the policy cache. Must be inside a writable volume; `git clone` creates it on cold start. |

!!! note "Inputs from Argo CD"
    The trust context is **not** configured here — it's read from the
    `ARGOCD_APP_*` / `ARGOCD_ENV_*` variables Argo injects per generation. See
    [CLI & inputs](cli.md).

## Fail mode

Fail-closed is **fixed behavior**, not an environment variable — there is no
fail-open mode. The only availability softening is the stale-cache window (serve
last-known-good when the repo is unreachable but a cache exists). See
[Fail-closed](../concepts/fail-closed.md) and [Caching](../operations/caching.md).

## Pinned tool versions

The image bundles specific `kustomize` and `conftest` versions (see the
[`Dockerfile`](https://github.com/ahmetozer/argo-guard/blob/main/Dockerfile)).
Use the matching `conftest` version when authoring/testing policies locally.
