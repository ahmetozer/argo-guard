# Deploying argo-guard

1. Build & push the image:
   `docker build -t registry.corp.internal/argo-guard:v0.1.0 . && docker push registry.corp.internal/argo-guard:v0.1.0`
2. Create the plugin ConfigMap from `plugin.yaml`:
   `kubectl -n argocd create configmap argo-guard-plugin --from-file=plugin.yaml=deploy/plugin.yaml`
3. Apply `repo-server-patch.yaml` to the `argocd-repo-server` Deployment
   (via your Argo install's kustomize/Helm values; `var-files` and `plugins`
   volumes already exist in the stock repo-server pod).
4. Roll out: `kubectl -n argocd rollout restart deploy/argocd-repo-server`.

Failure of this sidecar pauses manifest generation only; running workloads in
target clusters are unaffected.
