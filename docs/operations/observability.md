# Observability

argo-guard surfaces its decisions through Argo CD's existing UI and logs — there
is no separate dashboard to run.

## Where violations show up

When a sync is blocked, the violation report is written to the plugin's stderr,
which Argo surfaces as the **operation/sync message** on the Application. The
report lists each finding:

```
argo-guard policy report
  DENY  [-] Service/web: LoadBalancer is not allowed
  WARN  [-] Deployment/web should set an owner label
  summary: 1 deny, 1 warn
```

- **DENY** lines mean the sync failed (exit 1).
- **WARN** lines are informational — they do not block (exit stays 0).
- A **stale-cache warning** appears when the policy repo was unreachable and
  last-known-good policies were served.

## Logs

The sidecar logs to the `argo-guard` container in the `argocd-repo-server` pod:

```bash
kubectl -n argocd logs deploy/argocd-repo-server -c argo-guard
```

Look here for clone/fetch activity, stale-cache warnings, and fail-closed errors
(exit 2 conditions).

## Suggested signals to alert on

argo-guard doesn't export metrics itself; derive these from repo-server logs or
sync results:

| Signal | Why it matters |
|---|---|
| Stale-cache warnings | Policy repo unreachable — rules may be delayed |
| Spike in exit-2 (internal errors) | Broken policy or render — investigate fast |
| Deny rate by repo/project | Detect a team repeatedly hitting a rule (or a too-strict rule) |
| First generation after pod restart slow | Expected cold-start clone — see [Caching](caching.md) |
