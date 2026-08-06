# Writing Rego

argo-guard evaluates your rules with [Conftest](https://www.conftest.dev/)
(OPA/Rego). This page covers the conventions specific to argo-guard.

## Always declare the dialect

Start **every** `.rego` file with:

```rego
package main
import rego.v1
```

`import rego.v1` pins the Rego v1 dialect, so `if` / `contains` / `in` parse
identically on every conftest version. Without it, a rule that works on a new
conftest can fail with `rego_parse_error: var cannot be used for rule name` on
an older pinned one. This is the single most common policy bug — see
[Troubleshooting](../operations/troubleshooting.md#policies-parse-locally-but-fail-in-the-cluster).

Use `package main` so Conftest collects your `deny`/`warn` rules by default.

## The three data sources

Know which namespace a value comes from — it's also the trust boundary:

| Expression | What it is | Trust |
|---|---|---|
| `input.*` | the **manifest** under test (one per document) | untrusted — this is what you police |
| `data.context.*` | the **trust context** from Argo (`repo`, `project`, `namespace`, `appLabels`) | trusted (spoof-proof) |
| `data.<key>` | values from the bundle's **`data.json`** (e.g. `data.trustedRepos`) | trusted (PR-reviewed) |

See [Trust model](../concepts/trust-model.md) for why context is in `data`, not `input`.

## deny vs warn

```rego
deny contains msg if { ... }   # fails the sync (exit 1)
warn contains msg if { ... }   # reported but does not block (exit stays 0)
```

Ship a new rule as `warn` first to see what it *would* block, then promote it to
`deny`. See [Fail-closed](../concepts/fail-closed.md).

## Transition policies

A `mode: transition` bundle receives a `ManifestChange` document:

```yaml
apiVersion: guard.ahmetozer.github.io/v1alpha1
kind: ManifestChange
spec:
  operation: Update # Create, Update, or Delete
  resource:
    apiVersion: apps/v1
    kind: Deployment
    namespace: payments
    name: api
  before: { ... }
  after: { ... }
```

For example, block new production scale-to-zero transitions while allowing
resources that were already at zero in the last successful sync:

```rego
previously_zero if {
    input.spec.before != null
    object.get(input.spec.before.spec, "replicas", 1) == 0
}

deny contains msg if {
    input.kind == "ManifestChange"
    input.spec.after.kind in {"Deployment", "StatefulSet"}
    object.get(input.spec.after.spec, "replicas", 1) == 0
    not previously_zero
    msg := sprintf("%s/%s cannot transition to replicas=0", [
        input.spec.after.kind,
        input.spec.after.metadata.name,
    ])
}
```

Scope the bundle to production using trusted `guard.yaml` context such as the
Argo project or destination namespace. Do not use developer-controlled resource
labels to grant a bypass.

### Inspecting other resources

Each changed resource remains the Conftest `input`. Transition evaluation also
exposes the complete desired states once through OPA data:

```text
data.transition.applicationName
data.transition.destination
data.transition.previousRevision
data.transition.currentRevision
data.transition.changes
data.transition.beforeResources
data.transition.afterResources
```

`applicationName` comes directly from Argo CD's `ARGOCD_APP_NAME` build
environment variable. It is therefore suitable for platform-controlled policy
exceptions; unlike a manifest label, application Git cannot spoof it.

`destination` comes from the trusted Argo CD Application and contains its
`server`, `name`, and `namespace` target fields. Use it to scope transition
rules to exact clusters rather than relying on manifest labels.

`changes` is a lightweight list of operation/resource identities. The resource
arrays include changed and unchanged rendered manifests, so a policy can check
references that survive another resource's deletion. For example, reject a
ServiceAccount deletion while a Deployment still refers to it:

```rego
deny contains msg if {
    input.kind == "ManifestChange"
    input.spec.operation == "Delete"
    input.spec.resource.kind == "ServiceAccount"

    some workload in data.transition.afterResources
    workload.kind == "Deployment"
    workload.spec.template.spec.serviceAccountName == input.spec.resource.name
    object.get(workload.metadata, "namespace", data.context.namespace) ==
        object.get(input.spec.resource, "namespace", data.context.namespace)

    msg := sprintf("ServiceAccount/%s is still referenced by Deployment/%s", [
        input.spec.resource.name,
        workload.metadata.name,
    ])
}
```

These arrays are rendered Git desired state, not live target-cluster inventory.
Runtime data is written with mode `0600` under the per-generation temporary
directory and removed before the process exits.

Resource identity is `API group + kind + namespace + metadata.name`. The full
`apiVersion` remains in `before`, `after`, and `resource`; changing an Ingress
from `networking.k8s.io/v1beta1` to `networking.k8s.io/v1` is therefore one
`Update`, while moving to a genuinely different API group is `Delete + Create`.

## Writing good messages

Each `deny`/`warn` is a string shown in the Argo UI. Make it actionable —
include the kind, name, and what to do:

```rego
deny contains msg if {
    input.kind == "Deployment"
    some c in input.spec.template.spec.containers
    not c.resources.limits.memory
    msg := sprintf("Deployment/%s container %q must set spec...resources.limits.memory",
                   [input.metadata.name, c.name])
}
```

## Reading the trust context

```rego
deny contains msg if {
    input.kind == "Namespace"
    not data.context.repo == data.trustedRepos[_]   # exempt trusted repos
    msg := sprintf("Namespace creation only allowed from trusted infra repos (got %s)",
                   [data.context.repo])
}
```

See the [Cookbook](cookbook.md) for ready-to-use rules and
[Testing policies](testing.md) for how to unit-test them.
