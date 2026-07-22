# Configuration

argo-guard is configured entirely through environment variables on the sidecar
container. There is no config file.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `GUARD_POLICY_REPO` | _(empty)_ | Clone URL of the policy Git repo. If empty, argo-guard uses an existing `GUARD_POLICY_CACHE` directory as-is and never runs Git — either a plain policy tree (local mode) or a flattened ConfigMap mount (`GUARD_POLICY_FLAT`, the zero-credential production mode). |
| `GUARD_POLICY_FLAT` | _(unset)_ | Set to `true` when `GUARD_POLICY_CACHE` is a flattened ConfigMap mount: `__` in a key maps to `/` (`global__restrictions.rego` → `global/restrictions.rego`), expanded per generation. Any value other than unset/`true` fails closed (exit 2), as does combining it with a non-empty `GUARD_POLICY_REPO`. `GUARD_POLICY_TTL`/`USERNAME`/`PASSWORD` are irrelevant in this mode. See [Policy repo setup](../install/policy-repo-setup.md#zero-credential-delivery-via-configmap). |
| `GUARD_POLICY_REF` | `main` | Branch or tag to check out in the policy repo. Pin to a tag for explicit promotion. |
| `GUARD_POLICY_TTL` | `60s` | How long a cached policy checkout is considered fresh before a refresh is attempted. Accepts Go durations (`30s`, `5m`) or a bare integer (seconds). |
| `GUARD_POLICY_CACHE` | `/var/cache/argo-guard/policies` | Local path for the policy cache. Must be inside a writable volume; `git clone` creates it on cold start. |
| `GUARD_POLICY_PATH` | _(repo root)_ | Subdirectory inside the policy repo where `guard.yaml` and the bundle directories live (e.g. `policies/prod`). Must be a relative path that stays inside the repo; absolute paths or `..` escapes fail closed. Also applies in local mode, relative to `GUARD_POLICY_CACHE`. |
| `GUARD_POLICY_PASSWORD` | _(empty)_ | Password or token for cloning a private policy repo over HTTPS. When set, git authenticates via an inline credential helper that reads it from the environment at runtime — it never appears in the URL, the command line, or error output. Source it from a Secret with `valueFrom: secretKeyRef` ([Policy repo setup](../install/policy-repo-setup.md#private-policy-repos-git-delivery)). |
| `GUARD_POLICY_USERNAME` | `x-access-token` | Username paired with `GUARD_POLICY_PASSWORD`. The default works for GitHub token auth; ignored when no password is set. |
| `GUARD_ARGOCD_NAMESPACE` | `argocd` | Namespace containing the Argo CD Application CR. Used only when a matching bundle has `mode: transition`. |

!!! note "Inputs from Argo CD"
    The trust context is **not** configured here — it's read from the
    `ARGOCD_APP_*` / `ARGOCD_ENV_*` variables Argo injects per generation. See
    [CLI & inputs](cli.md).

## Transition policy prerequisites

Transition mode is activated by a matching `mode: transition` bundle. It needs:

- `provideGitCreds: true` in the ConfigManagementPlugin so the previous Git
  revision can be fetched during generation;
- read-only `get` permission on `applications.argoproj.io` in
  `GUARD_ARGOCD_NAMESPACE`;
- Application revision history enabled (`spec.revisionHistoryLimit` must not be
  zero).

Missing history on a never-synced Application is treated as an empty previous
state, so all requested resources are `Create` changes. Missing permissions,
disabled history, unsupported multi-source history, or an unreachable previous
revision fail closed.

## Fail mode

Fail-closed is **fixed behavior**, not an environment variable — there is no
fail-open mode. The only availability softening is the stale-cache window (serve
last-known-good when the repo is unreachable but a cache exists). See
[Fail-closed](../concepts/fail-closed.md) and [Caching](../operations/caching.md).

## Pinned tool versions

The image bundles specific `kustomize` and `conftest` versions (see the
[`Dockerfile`](https://github.com/ahmetozer/argo-guard/blob/main/Dockerfile)).
Use the matching `conftest` version when authoring/testing policies locally.
