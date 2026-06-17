# Trust model

This is the page that explains *why* argo-guard can be trusted to enforce policy — and how it can safely **grant** elevated privileges to some repos without those privileges being forgeable.

## The core principle: trust comes from Argo, not from the manifest

When argo-guard evaluates an application, it knows two very different categories of information:

| Category | Source | Trustworthy? |
|---|---|---|
| **The manifests** | `kustomize build` output (developer-controlled) | ❌ This is exactly what we are policing — it cannot be self-certifying. |
| **The trust context** | Argo CD environment variables (`ARGOCD_APP_*`) | ✅ Set by Argo from the `Application` resource, which lives in the Argo control plane. |

A developer editing their YAML can change *the manifests*. They **cannot** change the repo URL Argo recorded for the `Application`, the AppProject it belongs to, or the destination namespace — those come from the `Application` object, governed by whoever has rights to create/edit Applications in Argo.

That asymmetry is the whole foundation: **policy decisions key off the trust context, never off manifest content.**

### The trust context

```json
{
  "repo": "https://git.corp/infra/platform.git",
  "project": "platform",
  "namespace": "ingress",
  "appLabels": { "tier": "frontend" }
}
```

| Field | Argo source | Spoof-proof? |
|---|---|---|
| `repo` | `ARGOCD_APP_SOURCE_REPO_URL` | **Strongest anchor** — set from the Application's source, governed by repo write-access |
| `project` | `ARGOCD_APP_PROJECT_NAME` | Strong — the AppProject is an Argo RBAC boundary |
| `namespace` | `ARGOCD_APP_NAMESPACE` | Strong |
| `appLabels` | `ARGOCD_ENV_*` plugin parameters | **Weaker** — set by the Application author; treat as a convenience selector, not a security boundary |

!!! warning "Labels are author-controlled"
    Argo does not expose Application labels to a CMP, so argo-guard derives "labels" from `ARGOCD_ENV_*` plugin parameters that the Application author sets. Use labels to *route* policy, not to *grant* privilege. For privilege decisions, key off `repo` (and `project`).

## Two mechanisms, two jobs

argo-guard separates *which rules run* from *what those rules decide*.

### 1. Bundle selection — additive, only ever stricter

The [`guard.yaml`](../policies/guard-yaml.md) registry decides which policy bundles apply to an app, by matching on the trust context. **Every matching bundle applies, and violations are the union.** Matching can only *add* rules — it can make policy stricter, never looser. This is the right default for a security gate: a misconfigured selector fails toward *more* enforcement.

So you **cannot** grant a repo extra freedom just by selecting bundles. That is deliberate.

### 2. Context injection — how you *grant* privileges

To let a trusted repo do something others can't, the trust context is injected into every Rego evaluation as **`data.context`**, and rules carry their own exemptions:

```rego
package main
import rego.v1

# Cluster-scoped RBAC is denied — UNLESS the deploying repo is trusted.
deny contains msg if {
    input.kind in {"ClusterRole", "ClusterRoleBinding"}
    not context_repo_trusted
    msg := sprintf("%s/%s: cluster RBAC only allowed from trusted infra repos",
                   [input.kind, input.metadata.name])
}

context_repo_trusted if {
    some r in data.trustedRepos      # from the bundle's data.json
    r == data.context.repo           # spoof-proof: comes from Argo, not the manifest
}
```

Here `data.context.repo` is the trusted repo URL and `data.trustedRepos` is an allowlist shipped in the bundle's `data.json`. Because the comparison is against a value Argo provided, **a developer cannot self-exempt by editing their manifest** — they would have to get their repo added to the PR-reviewed allowlist.

Granting a repo elevated rights is therefore a **one-line data change** in the policy repo, reviewed like any other PR. See [Trusted repos](../policies/trusted-repos.md).

## Why `data.context`, not `input.context`?

In Conftest/OPA, `input` is the document under test — i.e. the **manifest**. If the trust context lived in `input`, it would be co-mingled with developer-controlled data. argo-guard passes the context (and each bundle's `data.json`) to Conftest via `--data`, so it lands under `data.*`:

- `input.*` → the manifest being validated (untrusted)
- `data.context.*` → the trust context (trusted, from Argo)
- `data.trustedRepos`, etc. → allowlists from the bundle's `data.json` (trusted, PR-reviewed)

This keeps the trust boundary visible in the rule itself.

## What this model does *not* protect against

- **Who can create Applications in Argo.** argo-guard trusts the repo/project Argo reports. If an attacker can create an Application pointing a trusted repo at arbitrary content, that's an Argo RBAC problem upstream of argo-guard. Lock down AppProject source repositories.
- **Resources not deployed through Argo CD.** Out of scope by design.
- **Manifest-level bypass.** There is none — see [Break-glass](../operations/break-glass.md). Emergency exemptions are made in the PR-controlled policy repo, keeping trust where it already lives.
