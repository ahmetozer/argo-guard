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
