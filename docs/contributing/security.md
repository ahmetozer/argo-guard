# Security

## Threat model in one paragraph

argo-guard enforces policy at Argo CD render time. Its trust boundary is the
line between **manifest content** (developer-controlled, untrusted — this is what
it polices) and the **trust context** (`repo`, `project`, `namespace` from
Argo's environment — trusted). Policy decisions, especially privilege *grants*,
must key off the trust context, never off manifest content. See
[Trust model](../concepts/trust-model.md).

## What's trusted vs untrusted

| Trusted | Untrusted |
|---|---|
| `data.context.*` (from `ARGOCD_APP_*`) | `input.*` (the rendered manifest) |
| Allowlists in bundle `data.json` (PR-reviewed) | `ARGOCD_ENV_*`-derived `appLabels` (author-set) |
| The policy Git repo (PR-controlled) | The application source repo content |

## Properties argo-guard provides

- **Spoof-proof trust** — a developer cannot grant themselves privileges by
  editing YAML; grants require a PR to the policy repo's allowlists.
- **No manifest-level bypass** — break-glass goes through the policy repo only
  ([Break-glass](../operations/break-glass.md)).
- **Fail-closed** — errors fail the sync rather than emitting unchecked manifests
  ([Fail-closed](../concepts/fail-closed.md)).
- **Limited blast radius** — runs in the repo-server; a failure pauses syncs, it
  does not affect the target cluster control plane or running workloads.

## What argo-guard relies on you to secure

- **Argo RBAC / AppProject source restrictions.** argo-guard trusts the repo and
  project Argo reports. If someone can create an `Application` that points a
  *trusted* repo URL at arbitrary content, that's an upstream Argo RBAC gap.
  Restrict which repositories each AppProject may source from.
- **Policy repo write access.** Anyone who can merge to the policy repo can
  change enforcement. Require review.
- **Image supply chain.** Pin and verify the argo-guard image and its bundled
  `kustomize`/`conftest` versions.

## Reporting a vulnerability

Open a private security advisory on the GitHub repository (Security → Report a
vulnerability) rather than a public issue.
