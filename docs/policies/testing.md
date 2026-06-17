# Testing policies

Policies are code — test them where they live, in the policy repo, with
`conftest verify`. A policy change that breaks its own tests should never merge.

## Anatomy of a policy test

Tests live alongside the rules (e.g. `global/restrictions_test.rego`), use
`package main`, and start with `test_`. Use `with` to inject `input` and `data`:

```rego
package main
import rego.v1

test_loadbalancer_denied if {
    some msg in deny with input as {
        "kind": "Service", "metadata": {"name": "web"},
        "spec": {"type": "LoadBalancer"},
    }
    contains(msg, "LoadBalancer is not allowed")
}
```

## Test both sides of an exemption

For trusted-repo rules, prove the **denied** and the **allowed** branch — this is
what would have caught a broken exemption:

```rego
test_clusterrole_denied_for_untrusted_repo if {
    count(deny) > 0 with input as {"kind": "ClusterRole", "metadata": {"name": "x"}}
        with data.context as {"repo": "https://git.corp/team-a/app.git"}
        with data.trustedRepos as ["https://git.corp/infra/platform.git"]
}

test_clusterrole_allowed_for_trusted_repo if {
    count(deny) == 0 with input as {"kind": "ClusterRole", "metadata": {"name": "x"}}
        with data.context as {"repo": "https://git.corp/infra/platform.git"}
        with data.trustedRepos as ["https://git.corp/infra/platform.git"]
}
```

## Running

```bash
conftest verify --policy global/ --data global/
# N tests, N passed
```

!!! warning "Pin the conftest version"
    Run `conftest verify` in your policy repo's CI with the **same conftest
    version** the argo-guard image uses. Otherwise a Rego-dialect difference can
    let a policy pass locally and fail in the cluster — see
    [Troubleshooting](../operations/troubleshooting.md#policies-parse-locally-but-fail-in-the-cluster).

## Suggested CI gate (policy repo)

```yaml
- run: conftest verify --policy global/ --data global/
# repeat per bundle dir, or script a loop over your bundles
```

Block merges on this check so a rule can't ship if its own tests fail.
