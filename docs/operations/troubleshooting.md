# Troubleshooting

Symptoms below are grouped by what you see in the Argo UI or the sidecar logs.
Recall the [exit-code contract](../reference/exit-codes.md): **1 = policy
violation**, **2 = internal/fail-closed error**.

## Every sync suddenly fails with exit 2

A `2` means argo-guard could not complete evaluation, so it failed closed. Check
the sidecar logs (`kubectl -n argocd logs deploy/argocd-repo-server -c
argo-guard`) for the cause:

- **`cold-start clone of policy repo failed`** — the sidecar can't reach
  `GUARD_POLICY_REPO`, and there's no cache yet (e.g. right after a pod restart).
  Fix connectivity/credentials to the policy repo, then retry.
- **`parse guard.yaml` / `read guard.yaml`** — the registry is missing or
  malformed at the repo root. Validate it locally.
- **`conftest execution failed`** — a broken policy (see the next section).
- **`no policy bundles matched`** — your `guard.yaml` produced an empty
  selection. Add a `match: {}` global baseline (see [Scoping](../concepts/scoping.md)).

## Policies parse locally but fail in the cluster

**Symptom:** `conftest verify` passes on your laptop, but in CI or the sidecar
you see:

```
rego_parse_error: var cannot be used for rule name
```

**Cause:** a **conftest/OPA version mismatch**. Newer conftest (OPA 1.x) defaults
to the **Rego v1** dialect, where `deny contains msg if { ... }` and `... if {
}` are valid. Older pinned conftest (e.g. 0.56.0 / OPA 0.69) defaults to **Rego
v0** and rejects that syntax unless you opt in.

**Fix:** declare the dialect explicitly at the top of every `.rego` file:

```rego
package main
import rego.v1
```

`import rego.v1` is accepted by OPA 0.59+ and is a no-op on OPA 1.x, so it parses
identically everywhere. Then **pin the same conftest version** in your policy
repo's CI as the argo-guard image uses, so "passes locally" means "passes in the
cluster." See [Writing Rego](../policies/writing-rego.md).

## A trusted repo's ClusterRole is still denied

You expect a trusted infra repo to be exempt, but it's blocked anyway.

- Confirm the repo URL in `data.json`'s `trustedRepos` **exactly** matches
  `ARGOCD_APP_SOURCE_REPO_URL` (trailing `.git`, `https://` vs `git@`, case).
  The comparison is exact string equality.
- Confirm the bundle's `data.json` is actually loaded: argo-guard passes each
  selected bundle dir to conftest via **both `--policy` and `--data`**. If you
  invoke conftest yourself, remember `--policy <dir>` alone does **not** load
  `data.json` — you must also pass `--data <dir>`.
- Verify the rule reads `data.context.repo` (trusted) and not
  `input.context.repo` (which doesn't exist — `input` is the manifest). See
  [Trust model](../concepts/trust-model.md).

## A rule never fires (or always fires)

- Reading the manifest? Use `input.kind`, `input.spec...`.
- Reading the trust context? Use `data.context.repo`, `.project`, `.namespace`,
  `.appLabels`.
- Reading an allowlist? Use `data.<key>` from the bundle's `data.json`.
- Test the rule in isolation with `conftest verify` and `with input as {...}` /
  `with data.context as {...}` overrides — see [Testing policies](../policies/testing.md).

## My label-based bundle doesn't match

Argo CD does **not** pass Application labels to a CMP. argo-guard derives
`appLabels` from `ARGOCD_ENV_*` plugin parameters set on the Application's
`spec.source.plugin.env`. If your label match never fires, the Application
probably isn't setting the corresponding `ARGOCD_ENV_<KEY>` parameter. Remember
labels are author-controlled and should route policy, not grant privilege
([Trust model](../concepts/trust-model.md)).

## Stale-cache warnings in the report

```
WARNING: policy cache is stale (last-known-good served; repo unreachable)
```

argo-guard couldn't refresh the policy repo and is serving the last successful
fetch. Syncs still work, but **newly tightened rules are delayed**. Restore
connectivity to `GUARD_POLICY_REPO`; the next successful fetch clears it. See
[Caching](caching.md).

## The plugin isn't being used at all

- Confirm the app has a `kustomization.yaml` (that's the discovery glob).
- Confirm the `ConfigManagementPlugin` ConfigMap is mounted into the sidecar and
  the sidecar container is healthy in the `argocd-repo-server` pod.
- Check the repo-server can see the plugin socket (the `var-files`/`plugins`
  mounts in the patch).
