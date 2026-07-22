# Development

## Repo layout

```
cmd/argo-guard/        # CMP `generate` entrypoint; wires real subprocesses
internal/
  trust/               # trust context from Argo env vars
  match/               # match/exclude selection DSL + evaluator
  bundles/             # guard.yaml registry + bundle selection
  policyrepo/          # policy Git repo cache (TTL, last-known-good)
  render/              # kustomize build + manifest parsing
  argocd/              # last successful revision from Application history
  apprepo/              # fetch/render an application Git revision
  transition/           # desired-state diff → ManifestChange documents
  evaluate/            # conftest invocation, data.context injection
  emit/                # stdout manifests / stderr report
  generate/            # pipeline orchestration + exit-code contract
deploy/                # Dockerfile inputs: plugin.yaml, repo-server patch
examples/policies/     # sample policy repo (doubles as the e2e fixture)
e2e/                   # build-tagged end-to-end tests
docs/                  # this documentation (Material for MkDocs)
```

## Prerequisites

- Go (version per `go.mod`).
- `kustomize` and `conftest` on `PATH` for the e2e tests — **pinned to the same
  versions as the Dockerfile** to avoid Rego-dialect drift.

## Build, vet, test

```bash
go build ./...
go vet ./...
go test ./...                 # unit tests (e2e excluded by build tag)
go test -tags e2e ./e2e/      # end-to-end: real kustomize → conftest pipeline
```

The external commands (`kustomize`, `conftest`, `git`) are injected as function
dependencies, so every package is unit-testable without the real binaries; the
e2e suite exercises the real ones.

## Run against a fixture locally

```bash
# argo-guard builds the current working directory (like the CMP server, which
# runs the plugin inside the app source path), so cd into the fixture first.
REPO_ROOT="$PWD"
cd e2e/fixtures/bad-app && \
GUARD_POLICY_REPO= \
GUARD_POLICY_CACHE="$REPO_ROOT/examples/policies" \
ARGOCD_APP_SOURCE_REPO_URL="https://git.corp/team-a/app.git" \
go run "$REPO_ROOT/cmd/argo-guard" generate ; echo "exit: $?"
```

Empty `GUARD_POLICY_REPO` makes argo-guard treat `GUARD_POLICY_CACHE` as the
policy root (no Git) — handy for iterating locally.

## Docs

```bash
pip install -r docs/requirements.txt
mkdocs serve              # live preview at http://127.0.0.1:8000
mkdocs build --strict     # what CI runs (fails on broken links/nav)
```
