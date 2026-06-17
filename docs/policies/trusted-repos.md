# Trusted repos

Some repositories — your platform/infra GitOps repos — legitimately need to
deploy things ordinary product apps must not: `Namespace`s, `ClusterRole`s,
CRDs, webhooks. argo-guard lets you **grant** those repos elevated rights without
that grant being forgeable by manifest content.

## Why this is safe

The grant keys off `data.context.repo`, which comes from Argo
(`ARGOCD_APP_SOURCE_REPO_URL`), **not** from the manifest. A developer cannot
edit their YAML to claim a trusted repo URL. The only way to become trusted is
to be added to the allowlist via a reviewed PR in the policy repo. See
[Trust model](../concepts/trust-model.md).

## The pattern

**1. List trusted repos in the bundle's `data.json`:**

```json title="global/data.json"
{
  "trustedRepos": [
    "https://git.corp/infra/platform.git",
    "https://git.corp/infra/clusters.git"
  ]
}
```

**2. Write the rule with an exemption:**

```rego title="global/restrictions.rego"
package main
import rego.v1

deny contains msg if {
    input.kind in {"ClusterRole", "ClusterRoleBinding", "Namespace"}
    not repo_trusted
    msg := sprintf("%s/%s: only trusted infra repos may create this kind",
                   [input.kind, input.metadata.name])
}

repo_trusted if {
    some r in data.trustedRepos
    r == data.context.repo
}
```

Now a `ClusterRole` from `https://git.corp/infra/platform.git` passes, while the
same manifest from any other repo is denied.

**3. Granting a new repo is a one-line PR** — add its URL to `trustedRepos`. No
code change, no image rebuild.

## Tips

- The match is **exact string equality**. Make sure the listed URL matches what
  Argo reports exactly (trailing `.git`, scheme, case). If unsure, check the
  repo-server logs or temporarily emit `data.context.repo` in a `warn`.
- Ensure the bundle's `data.json` is actually loaded — argo-guard passes each
  selected bundle dir to conftest via both `--policy` **and** `--data`. If you
  test by hand, pass `--data <bundle-dir>` too.
- Keep `trustedRepos` in the bundle that owns the restrictive rule, so the data
  and the rule that reads it travel together.
