# Deploy

argo-guard is deployed as a sidecar on `argocd-repo-server`, plus a plugin
config and a policy ConfigMap mount. The repo ships ready-to-apply artifacts under
[`deploy/`](https://github.com/ahmetozer/argo-guard/tree/main/deploy).

## 1. Build & push the image (or use the published one)

```bash
docker build -t registry.example.com/argo-guard:v0.1.0 .
docker push registry.example.com/argo-guard:v0.1.0
# or use ghcr.io/ahmetozer/argo-guard:<tag>
```

## 2. Install the plugin config

The `ConfigManagementPlugin` manifest tells Argo how to discover and generate.
Mount it into the sidecar as a ConfigMap:

```bash
kubectl -n argocd create configmap argo-guard-plugin \
  --from-file=plugin.yaml=deploy/plugin.yaml
```

The supplied plugin config enables `provideGitCreds` so transition bundles can
fetch the last successfully synced revision of the same application repo.

Apply the narrow read-only RBAC used to resolve that revision from Application
history:

```bash
kubectl apply -f deploy/application-reader-rbac.yaml
```

It grants the repo-server ServiceAccount only `get` on
`applications.argoproj.io` in the `argocd` namespace. It does not grant access
to target clusters.

```yaml title="deploy/plugin.yaml"
apiVersion: argoproj.io/v1alpha1
kind: ConfigManagementPlugin
metadata:
  name: argo-guard
spec:
  version: v1
  provideGitCreds: true
  discover:
    find:
      glob: "**/kustomization.yaml"   # every Kustomize app → enforced by default
  generate:
    command: ["argo-guard", "generate"]
```

The `kustomization.yaml` glob means enforcement is **on by default** for every
Kustomize app — no per-app opt-in to forget.

## 3. Add the sidecar to `argocd-repo-server`

Apply [`deploy/repo-server-patch.yaml`](https://github.com/ahmetozer/argo-guard/blob/main/deploy/repo-server-patch.yaml)
via your Argo install (Kustomize patch or Helm values). It adds the `argo-guard`
container, the shared plugin socket, the plugin-config mount, and the policy
ConfigMap mount ([zero-credential delivery](policy-repo-setup.md#zero-credential-delivery-via-configmap) —
mind its bootstrap step):

```yaml
containers:
  - name: argo-guard
    image: ghcr.io/ahmetozer/argo-guard:v0.1.0
    command: ["/var/run/argocd/argocd-cmp-server"]
    securityContext: { runAsNonRoot: true, runAsUser: 999 }
    env:
      - { name: GUARD_POLICY_FLAT,  value: "true" }
      - { name: GUARD_POLICY_CACHE, value: "/etc/argo-guard/policies" }
      - { name: GUARD_ARGOCD_NAMESPACE, value: "argocd" }
    volumeMounts:
      - { name: var-files,         mountPath: /var/run/argocd }
      - { name: plugins,           mountPath: /home/argocd/cmp-server/plugins }
      - { name: argo-guard-config, mountPath: /home/argocd/cmp-server/config/plugin.yaml, subPath: plugin.yaml }
      - { name: cmp-tmp,           mountPath: /tmp }
      - { name: policy-flat,       mountPath: /etc/argo-guard/policies, readOnly: true }
```

To have the sidecar clone the policy repo itself instead (git delivery), swap
the env for `GUARD_POLICY_REPO`/`REF`/`TTL` plus a writable cache volume — see
[Policy repo setup](policy-repo-setup.md) and [Configuration](../reference/configuration.md).

See [Configuration](../reference/configuration.md) for every environment variable.

## 4. Roll out

```bash
kubectl -n argocd rollout restart deploy/argocd-repo-server
kubectl -n argocd rollout status  deploy/argocd-repo-server
```

If the sidecar is unhealthy, **only manifest generation pauses** — running
workloads in target clusters are unaffected. Rolling back is a normal
repo-server Deployment rollback.

## 5. Verify

Trigger a sync of a Kustomize app and confirm:

- A clean app syncs as before.
- An app that violates a policy fails the sync, with the violation report shown
  in the Argo UI's sync/operation message. See [Observability](../operations/observability.md).
