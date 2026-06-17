# argo-guard

**argo-guard** is an Argo CD [Config Management Plugin](https://argo-cd.readthedocs.io/en/stable/operator-manual/config-management-plugins/) (CMP) that validates the manifests your developers deploy **before** they reach a cluster — entirely inside `argocd-repo-server`, never as an in-cluster admission webhook.

It renders each Kustomize application, checks it against layered [Conftest](https://www.conftest.dev/)/Rego policies selected by a small declarative match language, and either emits the manifests (pass) or fails the sync with a readable report (violation).

## Why not an in-cluster admission controller?

Tools like Kyverno or Gatekeeper run inside the target cluster as admission webhooks. That has two problems argo-guard avoids:

- **Lockout risk** — a misconfigured webhook can reject *every* API request, including the ones you need to fix it. argo-guard runs in the repo-server during manifest generation; if it misbehaves, **syncs pause — running workloads and the control plane are untouched**.
- **Control-plane load** — every admission review adds hooks and latency to the API server. argo-guard does its work at GitOps render time, off the hot path of the cluster.

Since every deployment already flows through Argo CD, the render stage is the natural, safe place to enforce policy.

## The 30-second mental model

```
Argo Application (sync)
        │
        ▼
argocd-repo-server ── calls CMP ──► argo-guard
        │
        ├─ 1. kustomize build              → rendered manifests
        ├─ 2. build trust context          → repo, project, namespace, labels (from Argo env)
        ├─ 3. select policy bundles         → match/exclude DSL over the cached policy repo
        ├─ 4. conftest test                 → Rego rules, trust context injected as data.context
        │
   PASS │ emit manifests to stdout   │ VIOLATION → non-zero exit, report in Argo UI
```

Two properties make it trustworthy:

- **Spoof-proof trust** — the repo URL, project, and namespace come from Argo CD environment variables, never from the manifest content being validated. A developer cannot grant themselves privileges by editing their YAML.
- **Fail-closed** — any error (render failure, broken policy, unreachable policy repo on cold start) fails the sync rather than letting unchecked manifests through.

## Is this for you?

| You want… | argo-guard fits? |
|---|---|
| Restrict resource kinds and field values across many teams | ✅ |
| Enforce policy **before** anything touches the cluster | ✅ |
| Grant trusted infra repos elevated rights without trusting manifest content | ✅ |
| Real-time, in-cluster mutation/blocking of arbitrary API requests | ❌ use Kyverno/Gatekeeper |
| Policy on resources **not** deployed through Argo CD | ❌ out of scope |

## Pick your track

<div class="grid cards" markdown>

- :material-lightbulb-on: **Understand it** — start with [Architecture](concepts/architecture.md) and the [Trust model](concepts/trust-model.md).
- :material-server: **Operate it** — go to [Prerequisites](install/prerequisites.md) → [Deploy](install/deploy.md).
- :material-file-document-edit: **Write policies** — start at the [Quickstart](policies/quickstart.md) and the [Cookbook](policies/cookbook.md).
- :material-book-open-variant: **Look something up** — the [Reference](reference/configuration.md) section.

</div>
