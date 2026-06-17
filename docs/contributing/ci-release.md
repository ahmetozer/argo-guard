# CI & release

The repo has four GitHub Actions workflows under `.github/workflows/`.

## Workflows

| Workflow | Trigger | Does |
|---|---|---|
| `test.yml` | `pull_request` + `workflow_call` | Build, vet, unit tests, e2e tests, `conftest verify` on the example policies. On PRs also builds the image (no push). Reusable so the same gate guards every path to GHCR. |
| `publish-main.yml` | push to `main` | Calls `test.yml`, then builds & pushes the image to GHCR tagged `main` and `sha-<commit>`. |
| `release.yml` | push tag `v*` | Calls `test.yml`, builds & pushes tagged `{{version}}`, `{{major}}.{{minor}}`, and `latest` (stable only), then creates a GitHub Release with generated notes. |
| `docs.yml` | push/PR touching `docs/**` or `mkdocs.yml` | Builds the MkDocs site with `--strict`; deploys to GitHub Pages from `main`. |

The test gate lives in one reusable workflow so PR, main, and release can't drift
apart.

## Image tags (GHCR)

`ghcr.io/<owner>/argo-guard`:

- `main`, `sha-<commit>` — from main pushes.
- `1.2.3`, `1.2`, `latest` — from a `v1.2.3` tag (latest only for stable releases).

## Cutting a release

```bash
git tag v0.1.0
git push origin v0.1.0
```

This runs the full test gate, publishes the versioned image, and opens a GitHub
Release. Tags must be semver-shaped (`vX.Y.Z`).

## Package visibility

The first GHCR push creates the package **private**. Make it public in the
package settings if you want release images publicly pullable (or configure pull
secrets for consumers).

## Local lint of workflows

```bash
actionlint
```
