# argo-guard

**Pre-deployment policy guardrails for Argo CD — enforced in the repo-server, never in the cluster control plane.**

argo-guard is an Argo CD [Config Management Plugin](https://argo-cd.readthedocs.io/en/stable/operator-manual/config-management-plugins/) (CMP) that validates the manifests your developers deploy **before** they reach a cluster. It renders each Kustomize application, checks it against layered [Conftest](https://www.conftest.dev/)/Rego policies selected by a small declarative match language, and either emits the manifests (pass) or fails the sync with a readable report (violation).

## Why not an in-cluster admission controller?

Tools like Kyverno or Gatekeeper run inside the target cluster as admission webhooks. argo-guard avoids two problems with that:

- **No lockout risk** — a misconfigured webhook can reject every API request, including the ones you need to recover. argo-guard runs at GitOps render time; if it misbehaves, **syncs pause — running workloads and the control plane are untouched**.
- **No control-plane load** — it does its work off the cluster's hot path, in `argocd-repo-server`.

Since every deployment already flows through Argo CD, the render stage is the natural, safe place to enforce policy.

## How it works

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

## Features

- **Resource-type and field-level policy** in Rego (allowed kinds, required limits, no privileged, registry allowlists, replica caps, …).
- **Layered, composable scoping** — global + namespace + project + label + **git-repo** rule sets, all applying together; more matches → stricter.
- **Grant elevated privileges to trusted git repos** (infra repos that legitimately create cluster-scoped resources) without trusting manifest content.
- **GitOps-native policies** — rules live in their own Git repo, cached with a TTL and last-known-good fallback. No image rebuild to change a rule.
- **On by default** — every Kustomize app is enforced; no per-app opt-in to forget.

## Quick start

**1. Deploy the plugin** as a sidecar on `argocd-repo-server` (image, `ConfigManagementPlugin`, and patch are in [`deploy/`](deploy/)):

```bash
kubectl -n argocd create configmap argo-guard-plugin --from-file=plugin.yaml=deploy/plugin.yaml
# apply deploy/repo-server-patch.yaml via your Argo install, then:
kubectl -n argocd rollout restart deploy/argocd-repo-server
```

Point it at your policy repo with `GUARD_POLICY_REPO` / `GUARD_POLICY_REF` / `GUARD_POLICY_TTL`.

**2. Write a policy** in that repo:

```yaml
# guard.yaml
bundles:
  - dir: global
    match: {}        # applies to every app
```

```rego
# global/restrictions.rego
package main
import rego.v1

deny contains msg if {
    input.kind == "Service"
    input.spec.type == "LoadBalancer"
    msg := sprintf("Service/%s: LoadBalancer is not allowed", [input.metadata.name])
}
```

Test it with `conftest verify --policy global/ --data global/`, open a PR, and argo-guard enforces it on the next sync.

See the [documentation](#documentation) for deployment, the match DSL, the trusted-repo pattern, and a policy cookbook.

## Documentation

Full docs (Material for MkDocs) live in [`docs/`](docs/) and publish to GitHub Pages. Highlights:

- **Concepts** — [Architecture](docs/concepts/architecture.md), [Trust model](docs/concepts/trust-model.md), [Scoping](docs/concepts/scoping.md), [Fail-closed](docs/concepts/fail-closed.md)
- **Install & Operate** — [Deploy](docs/install/deploy.md), [Policy repo setup](docs/install/policy-repo-setup.md), [Caching](docs/operations/caching.md), [Troubleshooting](docs/operations/troubleshooting.md)
- **Policy authoring** — [Quickstart](docs/policies/quickstart.md), [guard.yaml & match DSL](docs/policies/guard-yaml.md), [Writing Rego](docs/policies/writing-rego.md), [Cookbook](docs/policies/cookbook.md)
- **Reference** — [Configuration](docs/reference/configuration.md), [Exit codes](docs/reference/exit-codes.md), [Match DSL grammar](docs/reference/match-dsl.md)

Preview locally:

```bash
pip install -r docs/requirements.txt
mkdocs serve
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `GUARD_POLICY_REPO` | _(empty)_ | Policy Git repo clone URL (empty = use the local cache dir as-is) |
| `GUARD_POLICY_REF` | `main` | Branch or tag to check out |
| `GUARD_POLICY_TTL` | `60s` | Cache freshness before a refresh is attempted |
| `GUARD_POLICY_CACHE` | `/var/cache/argo-guard/policies` | Local policy cache path |

Exit codes: `0` pass (manifests on stdout), `1` policy violation, `2` internal/fail-closed error.

## Development

```bash
go build ./...
go vet ./...
go test ./...              # unit tests
go test -tags e2e ./e2e/   # end-to-end (needs kustomize + conftest on PATH)
```

External tools (`kustomize`, `conftest`, `git`) are injected as function dependencies, so every package is unit-testable without them; the e2e suite exercises the real pipeline. See [docs/contributing/development.md](docs/contributing/development.md).

## CI & releases

| Workflow | Trigger | Result |
|---|---|---|
| Test | pull request | build · vet · unit + e2e tests · `conftest verify` · image build (no push) |
| Publish (main) | push to `main` | image to GHCR tagged `main`, `sha-<commit>` |
| Release | push tag `v*` | image tagged `{{version}}`, `{{major}}.{{minor}}`, `latest`; GitHub Release |

Image: `ghcr.io/ahmetozer/argo-guard`.

## License

argo-guard is licensed under the [Apache License 2.0](LICENSE).
