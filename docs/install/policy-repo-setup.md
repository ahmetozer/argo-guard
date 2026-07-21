# Policy repo setup

Policies live in their own Git repository — versioned and PR-reviewed like any
GitOps content, with no ConfigMap size limit and no image rebuild to change a
rule.

## Layout

```
argo-guard-policies/
├── guard.yaml                 # bundle registry: dir + match/exclude
├── global/                    # a bundle (match: {}, always applies)
│   ├── restrictions.rego
│   ├── restrictions_test.rego
│   └── data.json              # allowlists, e.g. trustedRepos
├── namespaces/
│   └── payments/ ...          # match: namespace = payments
└── projects/
    └── platform/ ...          # match: project = platform
```

The `guard.yaml` at the repo root maps each bundle directory to its match rules
— see [guard.yaml & match DSL](../policies/guard-yaml.md).

If the policies don't live at the repo root (e.g. the repo also holds other
manifests), set `GUARD_POLICY_PATH` to the subdirectory containing `guard.yaml`
— see [Configuration](../reference/configuration.md).

## Point the sidecar at it

Set these on the sidecar (see [Deploy](deploy.md)):

| Variable | Example | Notes |
|---|---|---|
| `GUARD_POLICY_REPO` | `https://git.corp/platform/argo-guard-policies.git` | The clone URL |
| `GUARD_POLICY_REF` | `main` | Pin to a branch or a tag |
| `GUARD_POLICY_TTL` | `60s` | Cache refresh interval — see [Caching](../operations/caching.md) |
| `GUARD_POLICY_PATH` | `policies/prod` | Optional subdirectory holding `guard.yaml` (defaults to the repo root) |

## Zero-credential delivery via ConfigMap

Instead of the sidecar cloning the repo, let **Argo CD itself** deliver the
policies — it already has credentials for your Git host. An Argo CD
Application syncs the policy folder into a ConfigMap; the sidecar mounts it
and runs with `GUARD_POLICY_FLAT=true` and no `GUARD_POLICY_REPO` at all.

ConfigMap keys cannot contain `/`, so files are stored under **flattened
keys**: `__` stands for `/` (`global/restrictions.rego` →
`global__restrictions.rego`). argo-guard expands them back into a tree per
generation.

**1. In the policy repo**, add `policies/kustomization.yaml`:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: argocd
generatorOptions:
  disableNameSuffixHash: true   # sidecar mounts by fixed name
configMapGenerator:
  - name: argo-guard-policies
    files:
      - guard.yaml
      - global__restrictions.rego=global/restrictions.rego
      - global__data.json=global/data.json
```

Every policy/data file addition or rename must update this list **in the same
PR** — an unlisted file is silently absent from enforcement. Leave
`*_test.rego` out (runtime-inert; the ConfigMap has a 1 MiB limit).

**2. An Argo CD Application** (manual sync is fine — policy changes then go
live from the console) pointing at that folder, destination namespace
`argocd`. See [`deploy/policy-application.yaml`](https://github.com/ahmetozer/argo-guard/blob/main/deploy/policy-application.yaml).

**3. The sidecar** mounts the ConfigMap read-only (no `subPath` — it would
freeze updates) and sets `GUARD_POLICY_FLAT=true`,
`GUARD_POLICY_CACHE=<mount path>`. This is the default wiring in
[`deploy/repo-server-patch.yaml`](https://github.com/ahmetozer/argo-guard/blob/main/deploy/repo-server-patch.yaml).

!!! warning "Bootstrap and break-glass"
    The policy Application contains a `kustomization.yaml`, so argo-guard's own
    discovery (`**/kustomization.yaml`) routes **it** through the plugin too.
    On first install the ConfigMap doesn't exist yet — the sidecar's volume
    blocks the repo-server pod, so the Application can't create it. Bootstrap
    (and recover from a policy that fails everything, including itself) with a
    direct apply from a checkout of the policy repo:

    ```shell
    kustomize build policies | kubectl -n argocd apply -f -
    ```

    See [Break-glass](../operations/break-glass.md).

??? note "Rejected alternative: `items[].path` volume remapping"
    A ConfigMap volume's `items[].path` may contain `/`, so the tree could be
    rebuilt in the Deployment spec with no flattening at all. Rejected as the
    default: the file list would live in the repo-server Deployment, so a
    policy-file rename needs a rollout in a different repo, and a forgotten
    `items` entry silently drops a rule from enforcement.

## Private policy repos (git delivery)

If the policy repo needs authentication, set `GUARD_POLICY_PASSWORD` (and
optionally `GUARD_POLICY_USERNAME`, default `x-access-token`) on the sidecar
from a Secret. If Argo CD already has credentials for the same Git host as a
repository or repo-creds Secret in the `argocd` namespace, reuse it directly —
no second copy of the token to rotate:

```yaml
env:
  - name: GUARD_POLICY_USERNAME
    valueFrom:
      secretKeyRef:
        name: argocd-repo-creds-example   # existing Argo CD repo-creds Secret
        key: username
  - name: GUARD_POLICY_PASSWORD
    valueFrom:
      secretKeyRef:
        name: argocd-repo-creds-example
        key: password
```

The token only needs read access to the policy repo. Credentials are supplied
to git through an inline credential helper that expands them at runtime, so
they never appear in the clone URL, process arguments, or git error output
(which argo-guard forwards to the Argo CD UI).

!!! note "GitHub App repo-creds"
    Argo CD repo-creds Secrets that use a GitHub App (`githubAppID`,
    `githubAppPrivateKey`, …) contain no `username`/`password` keys and cannot
    be reused this way — create a fine-grained PAT (Contents: Read-only, scoped
    to the policy repo) in its own Secret instead.

!!! tip "Pin to a tag for change control"
    Pointing `GUARD_POLICY_REF` at a tag (e.g. `policies-2026-06`) means policy
    changes only take effect when you move the tag — useful if you want an
    explicit promotion step rather than "merges to main go live in `TTL`."

## Recommended repo hygiene

- **Require PR review** on the policy repo — this is your real change-control
  and break-glass surface ([Break-glass](../operations/break-glass.md)).
- **Run policy tests in the policy repo's own CI** with `conftest verify` so a
  rule that breaks its own tests can't merge. See [Testing policies](../policies/testing.md).
- **Pin the same conftest version** your image uses, in that CI, to avoid
  Rego-dialect drift.
