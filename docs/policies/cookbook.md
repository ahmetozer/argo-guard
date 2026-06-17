# Cookbook

Copy-paste rules for common guardrails. Every snippet starts from:

```rego
package main
import rego.v1
```

Each recipe includes the rule and a test. Adjust kinds/fields to your needs, and
ship as `warn` first if you want a soft launch ([Writing Rego](writing-rego.md)).

---

## Forbid `Service.type: LoadBalancer`

```rego
deny contains msg if {
    input.kind == "Service"
    input.spec.type == "LoadBalancer"
    msg := sprintf("Service/%s: LoadBalancer is not allowed; use ingress", [input.metadata.name])
}
```

```rego
test_loadbalancer_denied if {
    some msg in deny with input as {"kind": "Service", "metadata": {"name": "web"}, "spec": {"type": "LoadBalancer"}}
    contains(msg, "LoadBalancer")
}
```

---

## Require memory & CPU limits on every container

```rego
deny contains msg if {
    input.kind in {"Deployment", "StatefulSet", "DaemonSet"}
    some c in input.spec.template.spec.containers
    not c.resources.limits.memory
    msg := sprintf("%s/%s container %q must set resources.limits.memory",
                   [input.kind, input.metadata.name, c.name])
}

deny contains msg if {
    input.kind in {"Deployment", "StatefulSet", "DaemonSet"}
    some c in input.spec.template.spec.containers
    not c.resources.limits.cpu
    msg := sprintf("%s/%s container %q must set resources.limits.cpu",
                   [input.kind, input.metadata.name, c.name])
}
```

```rego
test_missing_memory_limit_denied if {
    some msg in deny with input as {
        "kind": "Deployment", "metadata": {"name": "web"},
        "spec": {"template": {"spec": {"containers": [{"name": "app", "resources": {}}]}}},
    }
    contains(msg, "resources.limits.memory")
}
```

---

## Forbid privileged / privilege-escalating containers

```rego
deny contains msg if {
    input.kind in {"Deployment", "StatefulSet", "DaemonSet", "Pod"}
    containers := object.get(input, ["spec", "template", "spec", "containers"], input.spec.containers)
    some c in containers
    c.securityContext.privileged == true
    msg := sprintf("%s/%s container %q must not run privileged",
                   [input.kind, input.metadata.name, c.name])
}
```

```rego
test_privileged_denied if {
    some msg in deny with input as {
        "kind": "Pod", "metadata": {"name": "p"},
        "spec": {"containers": [{"name": "c", "securityContext": {"privileged": true}}]},
    }
    contains(msg, "privileged")
}
```

---

## Restrict images to an approved registry

Put the prefix in the bundle's `data.json` so it's data, not code:

```json title="global/data.json"
{ "allowedRegistryPrefix": "registry.corp.internal/" }
```

```rego
deny contains msg if {
    input.kind in {"Deployment", "StatefulSet", "DaemonSet", "Pod"}
    containers := object.get(input, ["spec", "template", "spec", "containers"], input.spec.containers)
    some c in containers
    not startswith(c.image, data.allowedRegistryPrefix)
    msg := sprintf("%s/%s container %q image %q must come from %q",
                   [input.kind, input.metadata.name, c.name, c.image, data.allowedRegistryPrefix])
}
```

```rego
test_foreign_registry_denied if {
    some msg in deny
        with input as {"kind": "Pod", "metadata": {"name": "p"}, "spec": {"containers": [{"name": "c", "image": "docker.io/nginx"}]}}
        with data.allowedRegistryPrefix as "registry.corp.internal/"
    contains(msg, "must come from")
}
```

---

## Cap replicas

```rego
deny contains msg if {
    input.kind == "Deployment"
    input.spec.replicas > 10
    msg := sprintf("Deployment/%s: replicas %d exceeds the limit of 10",
                   [input.metadata.name, input.spec.replicas])
}
```

```rego
test_too_many_replicas_denied if {
    some msg in deny with input as {"kind": "Deployment", "metadata": {"name": "web"}, "spec": {"replicas": 25}}
    contains(msg, "exceeds the limit")
}
```

---

## Restrict cluster-scoped kinds to trusted repos

The flagship pattern — see [Trusted repos](trusted-repos.md) for the full
explanation.

```rego
deny contains msg if {
    input.kind in {"Namespace", "ClusterRole", "ClusterRoleBinding", "CustomResourceDefinition"}
    not repo_trusted
    msg := sprintf("%s/%s: cluster-scoped kinds only allowed from trusted infra repos",
                   [input.kind, input.metadata.name])
}

repo_trusted if {
    some r in data.trustedRepos
    r == data.context.repo
}
```

```rego
test_clusterrole_allowed_for_trusted_repo if {
    count(deny) == 0 with input as {"kind": "ClusterRole", "metadata": {"name": "x"}}
        with data.context as {"repo": "https://git.corp/infra/platform.git"}
        with data.trustedRepos as ["https://git.corp/infra/platform.git"]
}
```

---

## Namespace-scoped rule (route via guard.yaml, not Rego)

Prefer routing by [bundle selection](guard-yaml.md) over branching inside Rego.
Put this rule in a bundle matched to the namespace:

```yaml title="guard.yaml"
bundles:
  - dir: namespaces/payments
    match:
      namespace: payments
```

```rego title="namespaces/payments/rules.rego"
package main
import rego.v1

# Only applies to apps in the payments namespace, because of guard.yaml routing.
warn contains msg if {
    input.kind == "Deployment"
    not input.metadata.labels["pci-scope"]
    msg := sprintf("Deployment/%s in payments should set the pci-scope label", [input.metadata.name])
}
```
