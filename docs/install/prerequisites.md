# Prerequisites

Before deploying argo-guard you need:

- **Argo CD** with sidecar Config Management Plugins enabled (Argo CD 2.6+). argo-guard ships as a sidecar on `argocd-repo-server`, the standard CMP pattern.
- **A container image** — `ghcr.io/ahmetozer/argo-guard` (published by the release pipeline), or your own build of the [`Dockerfile`](https://github.com/ahmetozer/argo-guard/blob/main/Dockerfile). The image bundles pinned `kustomize` and `conftest` binaries.
- **A policy Git repository** that the sidecar can clone — containing a `guard.yaml` registry and your Rego bundles. See [Policy repo setup](policy-repo-setup.md).
- **Network access** from the repo-server pod to that policy repo (and to GHCR to pull the image).
- Applications managed with **Kustomize** (argo-guard discovers any app containing a `kustomization.yaml`).

## Permissions

- The sidecar needs no access to target clusters — it only reads the policy repo and renders locally.
- Image pull: if the GHCR package is private, configure an image pull secret; if public, none is needed.

## Version pinning

The image pins `kustomize` and `conftest` to specific versions. If you author policies locally, install the **same conftest version** to avoid Rego-dialect surprises — see [Troubleshooting](../operations/troubleshooting.md#policies-parse-locally-but-fail-in-the-cluster).
