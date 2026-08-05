# Deploying argo-guard

1. Build & push the image:
   `docker build -t registry.corp.internal/argo-guard:v0.1.0 . && docker push registry.corp.internal/argo-guard:v0.1.0`
2. Apply the read-only Application history RBAC:
   `kubectl apply -f deploy/application-reader-rbac.yaml`
3. Create the plugin ConfigMap from `plugin.yaml`:
   `kubectl -n argocd create configmap argo-guard-plugin --from-file=plugin.yaml=deploy/plugin.yaml`
4. Apply `repo-server-patch.yaml` to the `argocd-repo-server` Deployment
   (via your Argo install's kustomize/Helm values; `var-files` and `plugins`
   volumes already exist in the stock repo-server pod). The patch preserves
   `automountServiceAccountToken: false`, projects a token/CA only into
   argo-guard, and shares only the dedicated AskPass socket directory between
   repo-server and the sidecar. Their `/tmp` volumes remain separate.
5. Roll out: `kubectl -n argocd rollout restart deploy/argocd-repo-server`.

Failure of this sidecar pauses manifest generation only; running workloads in
target clusters are unaffected.
