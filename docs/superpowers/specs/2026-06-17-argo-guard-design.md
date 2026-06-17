# argo-guard — Design

**Date:** 2026-06-17
**Status:** Approved (pending implementation plan)

## Problem

Developers deploy Kubernetes manifests through Argo CD using Kustomize, with no
restrictions — they can deploy any resource type with any configuration.

We want a guardrail that limits **which resource types** and **what field-level
configuration** developers can deploy. Existing solutions (Kyverno, Gatekeeper,
etc.) run *in-cluster* as admission webhooks/controllers. We reject that approach
because:

- A misconfigured webhook can lock us out of our own cluster.
- It adds hooks and load to the control plane.

Since **all** deployments flow through Argo CD, we enforce policy at the Argo CD
manifest-generation stage — *before* anything reaches a target cluster's control
plane.

## Goals

- Enforce **resource-type** allow/deny rules (e.g. no `Namespace`, no `ClusterRole`,
  no `Service.type: LoadBalancer`).
- Enforce **field-level** policy (resource limits required, no `privileged`,
  registry allowlist, replica caps, etc.).
- **Layered, composable scoping**: global + namespace + project/team + label +
  **git-repo** rule sets, all applying together.
- **Grant elevated privileges to trusted git repos** (infra repos that legitimately
  deploy cluster-scoped resources) without trusting manifest content.
- Never touch the target cluster's control plane.
- Fail-closed by default.

## Non-Goals (v1)

- In-cluster enforcement of any kind.
- A custom policy DSL (we reuse Conftest/Rego).
- A manifest-content-controlled bypass mechanism.
- A live Argo CD + kind-cluster integration test (the e2e harness covers the CMP
  contract).

## Key Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Enforcement point | Argo CD **Config Management Plugin** (sidecar in `argocd-repo-server`) | Enforced inside Argo, covers every kustomize app, never touches target control plane |
| Policy engine | **Conftest (OPA / Rego)** | Proven CI/offline tool, single binary, max expressiveness |
| Scoping model | **Layered bundle selection + context injection** | Restrictions compose (stricter); trusted repos can be *granted* extra privileges |
| Policy delivery | **Dedicated policy Git repo, cached** | GitOps-native, no ConfigMap size limit, fast PR iteration, no image rebuild |
| Default when no bundle matches | **Global baseline always applies** | Never zero enforcement |
| Failure mode | **Fail-closed** | Security gate must not pass unchecked manifests |
| Stale cache | **Serve last-known-good + log warning** | Availability: a Git blip must not freeze all syncs |
| Break-glass | **Policy-repo break-glass only; no manifest-level bypass** | Trust stays where it lives (PR-controlled repo), not in spoofable manifest content |
| Implementation language | **Go** | Matching/caching/context logic needs unit tests; bash too fragile |

## Architecture

```
Argo Application (sync)
        │
        ▼
argocd-repo-server ── calls CMP ──► argo-guard sidecar
                                         │
                                         ├─ 1. kustomize build  → rendered manifests
                                         ├─ 2. build trust context (repo, project, ns, labels)
                                         ├─ 3. refresh/select policy bundles (cached policy repo)
                                         ├─ 4. conftest test (Rego + data.json, input.context)
                                         │
                                ┌────────┴────────┐
                          PASS  │                 │  VIOLATION
                                ▼                 ▼
                    emit manifests to       exit non-zero;
                    stdout (Argo applies)   report shown in Argo UI
```

Properties:

- **No target control-plane involvement** — all evaluation happens in repo-server
  during generation.
- **Spoof-proof trust** — trust context (repo URL, AppProject) comes from Argo env
  vars, not from the YAML being validated.
- **Blast radius = "syncs pause," not "cluster down"** — if argo-guard is unhealthy,
  only manifest generation is affected; running workloads are untouched.

## Scoping Model (two mechanisms)

**1. Bundle selection (coarse, additive — only ever stricter).**
Each bundle declares a `match` block; every matching bundle applies; violations are
the union. Match dimensions:

- Global — `match: {}` (always applies; the baseline)
- Namespace — `match: {namespaces: [payments, ingress]}`
- Project/team — `match: {projects: [team-a]}` (keyed off Argo AppProject)
- Label — `match: {labels: {tier: frontend}}`
- **Repo** — `match: {repos: ["https://git.corp/infra/platform.git"]}` (strongest
  trust anchor)

**2. Context injection (fine — enables *granting* privileges).**
The plugin injects the trust context into every Rego evaluation:

```json
input.context = {
  "repo": "https://git.corp/infra/platform.git",
  "project": "platform",
  "namespace": "ingress",
  "appLabels": {"tier": "frontend"}
}
```

Baseline rules carry their own exemptions, e.g.:

```rego
deny[msg] {
  input.kind == "ClusterRole"
  not input.context.repo == data.trustedRepos[_]   # trusted infra repos exempt
  msg := "ClusterRole only allowed from trusted infra repos"
}
```

The trusted-repo list lives in a `data.json` shipped with the bundle, so granting a
repo elevated privileges is a one-line, PR-reviewed data change.

## Components

### 1. `argo-guard` binary (Go) — CMP `generate` entrypoint

Orchestrates the pipeline; holds no policy logic. Internal units:

- **`render`** — `kustomize build` → parse multi-doc YAML → resource list.
- **`context`** — `ARGOCD_APP_*` env vars → trusted context block.
- **`bundles`** — trust context + `guard.yaml` → set of applicable bundle dirs.
- **`policyrepo`** — local cache of the policy Git repo (clone/fetch, ref pinning,
  TTL refresh, last-known-good fallback).
- **`evaluate`** — `conftest test` over rendered resources with selected bundles +
  `input.context`; collects deny/warn results.
- **`emit`** — pass → manifests to stdout; fail → violation report to stderr +
  non-zero exit.

### 2. Policy repo (separate Git repo, team-owned)

```
policies/
  global/              rego + data.json   (match: {})
  namespaces/payments/ ...                (match: {namespaces: [payments]})
  projects/platform/   ...                (match: {projects: [platform]})
  repos/trusted/       data.json          (trusted repo allowlist)
guard.yaml             # registry: bundle dirs → match blocks
```

`guard.yaml` maps bundle dirs to their match blocks, so adding a bundle is a pure
data change (no binary change).

### 3. Deployment artifacts

**3a. Container image (`Dockerfile`)** — bundles `kustomize`, `conftest`, `git`, and
the `argo-guard` binary; runs as user 999; the long-running process is Argo's
`argocd-cmp-server` (mounted via initContainer).

**3b. `ConfigManagementPlugin` spec** — mounted as a ConfigMap at
`/home/argocd/cmp-server/config/plugin.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ConfigManagementPlugin
metadata:
  name: argo-guard
spec:
  version: v1
  discover:
    find:
      glob: "**/kustomization.yaml"   # every kustomize app → default-on
  generate:
    command: ["argo-guard", "generate"]
```

**3c. `argocd-repo-server` patch** — adds the sidecar container, shared plugin
socket, and policy-cache volume. Config via env:

| Env var | Purpose |
|---|---|
| `GUARD_POLICY_REPO` | policy repo URL |
| `GUARD_POLICY_REF` | pinned branch/tag |
| `GUARD_POLICY_TTL` | cache refresh interval (e.g. `60s`) |
| `GUARD_FAIL_MODE` | `closed` (default) |

Deployed to the `argocd` namespace only.

**3d. Trust context source (consumed, not authored)** — Argo injects:

| Env var | Used for |
|---|---|
| `ARGOCD_APP_NAME` | logging / messages |
| `ARGOCD_APP_NAMESPACE` | namespace match |
| `ARGOCD_APP_PROJECT_NAME` | project/team match |
| `ARGOCD_APP_SOURCE_REPO_URL` | **repo trust anchor** |
| `ARGOCD_APP_SOURCE_PATH` | kustomize build path |
| app labels | label match |

## Data Flow (one generation request)

1. Argo triggers generation; repo-server finds `kustomization.yaml`, calls the
   sidecar `generate` with `ARGOCD_APP_*` env.
2. **Render** — `kustomize build $ARGOCD_APP_SOURCE_PATH`; parse to resource list.
   Failure → fail-closed.
3. **Build trust context** from env vars.
4. **Refresh policy cache** — if older than `GUARD_POLICY_TTL`, fetch + checkout
   `GUARD_POLICY_REF`. Fetch failure → last-known-good cache (logged). Cold start
   with no cache → fail-closed.
5. **Select bundles** — evaluate each `guard.yaml` match block against trust context;
   global always included.
6. **Evaluate** — `conftest test` per resource with the union of selected bundles,
   `input.context` available, trusted lists from `data.json`.
7. **Verdict & emit** — any `deny` → violation report to stderr + non-zero exit (sync
   fails, shown in Argo UI). `warn`-only/clean → manifests to stdout (Argo applies).

`warn` vs `deny` provides a soft-launch path: ship a rule as `warn`, observe, then
promote to `deny`.

## Failure Handling

| Failure | Behavior | Rationale |
|---|---|---|
| `kustomize build` fails | Fail sync, surface error | Broken manifests shouldn't deploy |
| Conftest error (bad Rego/panic) | Fail sync, surface error | Broken policy must not silently pass |
| Policy repo down, cache exists | Last-known-good + log warning | Git blip must not freeze all syncs |
| Policy repo down, cold start | Fail sync | Never run with zero policies |
| `guard.yaml` malformed | Fail sync | Can't trust bundle selection |
| No `kustomization.yaml` | Plugin doesn't match; Argo default handling | We only own kustomize apps |
| No bundle matches beyond global | Global baseline applies | Never zero enforcement |

**Stale-cache window (accepted):** when serving from cache during a Git outage,
newly tightened rules are delayed until the next successful fetch. Accepted with a
logged warning + metric, to avoid argo-guard becoming a single point of failure for
all syncs.

**Break-glass:** no manifest-level bypass. Emergency exemptions are made in the
PR-controlled policy repo (e.g. repo-scoped exemption entry), keeping trust where it
already lives.

## Testing Strategy

1. **Go unit tests** — `context`, `bundles` (exact selected-set assertions per match
   dimension + composition), `policyrepo` (TTL, last-known-good, cold-start
   fail-closed via local bare-repo fixture), `render`/`emit` (YAML round-trip,
   stdout/exit-code contract).
2. **Rego policy tests** (`conftest verify`, in the policy repo CI) — allow/deny
   fixtures per bundle; **trusted-repo exemption tests** (same `ClusterRole` denied
   for untrusted repo, allowed for trusted). Blocks merge if a policy breaks its own
   tests.
3. **End-to-end harness** — real `argo-guard generate` against golden fixture apps +
   a fixture policy repo, with simulated `ARGOCD_APP_*` env; asserts exit code +
   stdout manifests / stderr report.

Not building (v1): live Argo CD + kind integration test.
