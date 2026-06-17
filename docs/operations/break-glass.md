# Break-glass

Sometimes a policy blocks a legitimate, urgent deploy. argo-guard deliberately
has **no manifest-level bypass** — you cannot add an annotation to your app to
skip enforcement. That's by design: a manifest-controlled bypass would be
forgeable by exactly the people the gate is meant to constrain (see
[Trust model](../concepts/trust-model.md)).

Instead, emergency relief happens where trust already lives: **the
PR-controlled policy repo.**

## Options, fastest to safest

1. **Fix forward** — correct the manifest so it complies. Best when the policy is
   right and the manifest is wrong.
2. **Relax/scope the rule in the policy repo** — open a PR that narrows the rule
   or adds an exemption (e.g. add the repo to a `trustedRepos` allowlist, or add
   an `exclude` for a namespace). This is reviewable, auditable, and revertable.
3. **Move the rule to `warn` temporarily** — change `deny` to `warn` in a PR so
   the rule reports but stops blocking, then promote it back once the underlying
   issue is resolved.

Because all three go through the policy repo's PR review, every emergency change
is recorded and reversible — there is no silent, unaudited escape hatch.

## Make break-glass fast *before* you need it

- Keep policy-repo PR review lightweight enough that an urgent one-line
  exemption can be approved quickly.
- Consider a short `GUARD_POLICY_TTL` (or a documented "bump the policy tag"
  step) so an approved change takes effect promptly — see [Caching](caching.md).
- Use the `trustedRepos` / allowlist pattern so most "let this repo do X"
  requests are a **one-line data change**, not a rule rewrite. See
  [Trusted repos](../policies/trusted-repos.md).
