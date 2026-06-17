// Package generate orchestrates the full render→validate→emit pipeline that
// backs the `argo-guard generate` CMP command. It is fail-closed: any error
// returns exit code 2 and never emits manifests.
package generate

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/ahmetozer/argo-guard/internal/bundles"
	"github.com/ahmetozer/argo-guard/internal/emit"
	"github.com/ahmetozer/argo-guard/internal/evaluate"
	"github.com/ahmetozer/argo-guard/internal/render"
	"github.com/ahmetozer/argo-guard/internal/trust"
)

type Deps struct {
	Getenv         func(string) string
	Kustomize      render.KustomizeFunc
	Conftest       evaluate.ConftestFunc
	EnsurePolicies func() (root string, stale bool, err error)
	WorkDir        string
}

const (
	exitPass      = 0
	exitViolation = 1
	exitError     = 2
)

// Run executes the pipeline and returns the process exit code.
func Run(d Deps, stdout, stderr io.Writer) int {
	appPath := d.Getenv("ARGOCD_APP_SOURCE_PATH")
	if appPath == "" {
		appPath = "."
	}

	raw, _, err := render.Build(appPath, d.Kustomize)
	if err != nil {
		fmt.Fprintf(stderr, "argo-guard: %v\n", err)
		return exitError
	}

	ctx := trust.FromEnv(d.Getenv)

	policyRoot, stale, err := d.EnsurePolicies()
	if err != nil {
		fmt.Fprintf(stderr, "argo-guard: %v\n", err)
		return exitError
	}

	registry, err := bundles.Load(filepath.Join(policyRoot, "guard.yaml"))
	if err != nil {
		fmt.Fprintf(stderr, "argo-guard: %v\n", err)
		return exitError
	}
	selected := registry.Select(ctx)
	if len(selected) == 0 {
		fmt.Fprintf(stderr, "argo-guard: no policy bundles matched (guard.yaml must include a global match:{} baseline)\n")
		return exitError
	}

	res, err := evaluate.Run(raw, ctx, policyRoot, selected, d.WorkDir, d.Conftest)
	if err != nil {
		fmt.Fprintf(stderr, "argo-guard: %v\n", err)
		return exitError
	}

	if len(res.Denies) > 0 {
		emit.Report(stderr, res, stale)
		return exitViolation
	}
	if len(res.Warns) > 0 || stale {
		emit.Report(stderr, res, stale) // surface warnings but do not block
	}
	if err := emit.Success(stdout, raw); err != nil {
		fmt.Fprintf(stderr, "argo-guard: emit failed: %v\n", err)
		return exitError
	}
	return exitPass
}
