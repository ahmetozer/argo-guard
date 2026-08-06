# Desired-state transitions

A transition policy compares two Git-rendered desired states:

```text
last successful sync desired manifests -> requested revision desired manifests
```

It does **not** compare current live objects with their post-sync state. The
current revision arrives in the CMP workspace from repo-server. argo-guard reads
the previous source metadata from the newest `Application.status.history` entry,
fetches that exact Git revision, renders its recorded source path, and diffs the
two manifest sets. No target-cluster API is contacted.

Argo CD records history for successful, non-dry-run, non-selective full syncs.
A selective sync therefore does not advance this baseline. Drift, manual
changes, `Prune=false`, selective sync, hooks, failed or partial syncs, and
mutable remote Kustomize bases can all make the real cluster change differ from
the desired-state transition seen by policy.

## Application lookup scope

The lookup key is both `GUARD_ARGOCD_NAMESPACE` (default and supported deployment
value: `argocd`) and `ARGOCD_APP_NAME`. Different destination clusters and
destination namespaces do not collide because they are not used for this API
lookup. ApplicationSet-created Applications work like any other child
Application.

The same trusted Application name is exposed to transition policies as
`data.transition.applicationName`. Policy repositories can use it for narrowly
scoped, platform-controlled exceptions without trusting labels from application
manifests.

The target recorded in `Application.spec.destination` is exposed as
`data.transition.destination` with `server`, `name`, and `namespace` fields.
This lets policies scope enforcement to exact Argo-controlled destinations
without trusting labels from rendered manifests.

This deployment does not support Applications stored outside the `argocd`
namespace. Multi-source Applications and `revisionHistoryLimit: 0` fail closed,
because a single unambiguous previous desired source cannot be established.

## Repository credentials

`provideGitCreds: true` supplies short-lived credentials for the **current**
Application repository. It ships **commented out** in
[`deploy/plugin.yaml`](https://github.com/ahmetozer/argo-guard/blob/main/deploy/plugin.yaml)
— uncomment it only when a bundle uses `mode: transition`, so manifest-only
installs never hand application repo credentials to the sidecar.

Before starting Git, argo-guard normalizes and compares
the current and historical repository URLs. Equivalent forms such as an HTTPS
default port or a trailing `.git` are accepted. A different repository or
transport fails closed; current-repository credentials are never offered to the
historical URL.

The private fetch path is:

```text
repo-server credential store
  -> per-request AskPass nonce and GIT_ASKPASS
  -> mounted Argo CD AskPass client
  -> dedicated shared AskPass Unix socket
  -> repo-server returns credentials for the current repository
  -> git fetch --depth 1 <previous revision>
```

The AskPass socket is distinct from the CMP plugin socket. Credentials and the
nonce are not written to manifest output or included in argo-guard errors.

The fetch names a resolved commit SHA rather than a branch, because the branch
tip has usually moved past the recorded revision. The server must therefore
allow fetching a commit by SHA — see
[Git server capability](../reference/configuration.md#git-server-capability).
Where it does not, every matched sync fails closed at this step.

## Argo manifest-cache constraint

!!! danger "This is a bypass of the control, not a nuance"
    On a cache hit the plugin is never invoked, so transition policy does not
    run at all. Read this section before enabling a transition bundle; it is
    also summarized in the [Trust model](trust-model.md#what-this-model-does-not-protect-against)
    and the [README](https://github.com/ahmetozer/argo-guard#before-you-enable-transition-policies).

Argo CD's manifest cache includes the requested revision, but not the last
successful revision. This deployment therefore supports transition enforcement
only for an immutable, forward-only revision flow: every rollout, including a
revert, must be a new commit SHA (`A -> B -> C`, where C reverts B). With a new
SHA, Argo uses a new manifest-cache key and invokes generation for C.

Directly selecting an old SHA (`B -> A`), Argo UI/CLI rollback to an old
revision, mutable tags, and force-push are outside this guarantee and must be
prevented by repository and Argo RBAC/process controls. argo-guard does not add a
cache-busting controller, and a CMP cannot re-evaluate policy when repo-server
returns a cache hit without invoking it. If those rollback paths must be
supported, transition enforcement needs a controller or another layer that can
bind the cache key to the successful-sync baseline.
