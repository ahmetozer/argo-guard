# Architecture

argo-guard is a single Go binary that runs as a **Config Management Plugin sidecar** in the `argocd-repo-server` pod. For every Kustomize application Argo CD asks it to render, it owns manifest generation: it builds the manifests, validates them, and either returns them or fails.

## Where it runs

```
┌─ argocd-repo-server pod ───────────────────────────────┐
│  ┌─ repo-server ─┐   ┌─ argo-guard sidecar ─────────┐  │
│  │   (Argo)      │──▶│ argocd-cmp-server            │  │
│  │               │   │   └─ argo-guard generate     │  │
│  └───────────────┘   │       kustomize · conftest   │  │
│                      │       git (policy repo cache)│  │
│                      └──────────────────────────────┘  │
└────────────────────────────────────────────────────────┘
        (target clusters are never contacted here)
```

The sidecar shares the repo-server's plugin socket and a policy-cache volume. The long-running process is Argo's `argocd-cmp-server`; it invokes `argo-guard generate` per app.

## One generation, step by step

1. **Argo triggers generation.** The repo-server checks out the app's source revision, matches it (any app with a `kustomization.yaml`), and calls the plugin with the `ARGOCD_APP_*` environment variables.
2. **Render** — `kustomize build <source path>`; the multi-document output is parsed into resources and **kept verbatim** for later emission.
3. **Build trust context** — repo URL, project, namespace, and labels are read from the Argo environment. See [Trust model](trust-model.md).
4. **Refresh policy cache** — the policy Git repo is cloned/fetched into a local cache with a TTL. See [Caching](../operations/caching.md).
5. **Select bundles** — `guard.yaml` is evaluated against the trust context; every matching bundle applies. See [Scoping](scoping.md).
6. **Evaluate** — `conftest test` runs the selected bundles over the rendered manifests, with the trust context injected as `data.context`.
7. **Verdict** — any `deny` → write a report to stderr and exit non-zero (sync fails). Otherwise emit the manifests to stdout for Argo to apply; warnings and stale-cache notices are surfaced but do not block.

## Design properties

- **No target control-plane involvement.** Everything happens at render time in the repo-server. argo-guard never talks to a workload cluster's API server.
- **Blast radius is "syncs pause," not "cluster down."** If the sidecar is unhealthy, only manifest generation is affected; already-running workloads keep running.
- **Stateless per request.** The only shared state is the read-mostly policy cache. Each `generate` is an independent process invocation.
- **Fail-closed.** See [Fail-closed](fail-closed.md).

## Component map

| Package | Responsibility |
|---|---|
| `cmd/argo-guard` | CMP `generate` entrypoint; wires real `kustomize`/`conftest`/`git` subprocesses |
| `internal/render` | Run `kustomize build`, parse manifests |
| `internal/trust` | Build the trust context from Argo env vars |
| `internal/policyrepo` | Clone/fetch + TTL cache of the policy Git repo |
| `internal/bundles` + `internal/match` | `guard.yaml` registry + the match/exclude selection DSL |
| `internal/evaluate` | Invoke `conftest`, inject `data.context`, classify findings |
| `internal/emit` | Write manifests (pass) or a violation report (fail) |
| `internal/generate` | Orchestrate the pipeline and the exit-code contract |
