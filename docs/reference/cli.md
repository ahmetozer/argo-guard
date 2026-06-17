# CLI & inputs

argo-guard exposes a single command, invoked by Argo's `argocd-cmp-server`.

## Command

```
argo-guard generate
```

- Reads the application source path, renders it with `kustomize build`,
  validates it, and writes the result.
- **stdout**: rendered manifests (on pass). **stderr**: report/errors.
- Exit code per the [exit-code contract](exit-codes.md).

You normally never run this by hand — the `ConfigManagementPlugin` declares it as
the `generate.command`. For local experimentation you can run it directly with
the environment variables below set (and `GUARD_POLICY_REPO` empty to use a local
`GUARD_POLICY_CACHE` directory).

## Inputs from Argo CD

Argo injects these per generation; argo-guard consumes them to build the
[trust context](../concepts/trust-model.md) and locate the source:

| Variable | Used for |
|---|---|
| `ARGOCD_APP_SOURCE_PATH` | directory to run `kustomize build` in |
| `ARGOCD_APP_SOURCE_REPO_URL` | trust context `repo` — the trust anchor |
| `ARGOCD_APP_PROJECT_NAME` | trust context `project` |
| `ARGOCD_APP_NAMESPACE` | trust context `namespace` |
| `ARGOCD_ENV_<KEY>` | trust context `appLabels.<key>` (lowercased) |

## Plugin parameters → labels

Argo CD does not pass Application labels to a CMP. To make a label available for
[bundle selection](../policies/guard-yaml.md), set it as a plugin env parameter on
the Application:

```yaml
spec:
  source:
    plugin:
      name: argo-guard
      env:
        - name: TIER          # becomes appLabels.tier
          value: frontend
```

These are author-controlled — route policy with them, don't grant privilege
([Trust model](../concepts/trust-model.md)).

## Operator configuration

See [Configuration](configuration.md) for the `GUARD_POLICY_*` variables that
control the policy repo and cache.
