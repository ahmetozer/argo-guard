# Policy quickstart

Write, test, and ship your first rule in a few minutes. You'll need
[`conftest`](https://www.conftest.dev/) installed locally — **the same version
your argo-guard image uses** (see [Troubleshooting](../operations/troubleshooting.md#policies-parse-locally-but-fail-in-the-cluster)).

## 1. Add a bundle to `guard.yaml`

If you don't already have a global bundle, add one — it applies to every app:

```yaml title="guard.yaml"
bundles:
  - dir: global
    match: {}
```

## 2. Write a rule

```rego title="global/restrictions.rego"
package main
import rego.v1

deny contains msg if {
    input.kind == "Service"
    input.spec.type == "LoadBalancer"
    msg := sprintf("Service/%s: LoadBalancer is not allowed", [input.metadata.name])
}
```

- `input` is the **manifest** under test.
- `deny contains msg` adds a violation; any `deny` fails the sync.
- `import rego.v1` is **required** — it pins the Rego dialect so the rule parses
  on every conftest version.

## 3. Write a test

```rego title="global/restrictions_test.rego"
package main
import rego.v1

test_loadbalancer_denied if {
    some msg in deny with input as {
        "kind": "Service",
        "metadata": {"name": "web"},
        "spec": {"type": "LoadBalancer"},
    }
    contains(msg, "LoadBalancer is not allowed")
}
```

## 4. Run the tests

```bash
conftest verify --policy global/ --data global/
# 1 test, 1 passed
```

## 5. Ship it

Open a PR in your policy repo. Once merged (and within `GUARD_POLICY_TTL`, or
after you move the pinned tag), argo-guard enforces it on the next sync.

## Next

- The match language for routing bundles: [guard.yaml & match DSL](guard-yaml.md).
- More rule patterns: [Cookbook](cookbook.md).
- Granting a trusted repo an exemption: [Trusted repos](trusted-repos.md).
