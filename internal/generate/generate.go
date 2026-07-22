// Package generate orchestrates the full render→validate→emit pipeline that
// backs the `argo-guard generate` CMP command. It is fail-closed: any error
// returns exit code 2 and never emits manifests.
package generate

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ahmetozer/argo-guard/internal/bundles"
	"github.com/ahmetozer/argo-guard/internal/emit"
	"github.com/ahmetozer/argo-guard/internal/evaluate"
	"github.com/ahmetozer/argo-guard/internal/render"
	"github.com/ahmetozer/argo-guard/internal/transition"
	"github.com/ahmetozer/argo-guard/internal/trust"
)

type Deps struct {
	Getenv               func(string) string
	Kustomize            render.KustomizeFunc
	Conftest             evaluate.ConftestFunc
	EnsurePolicies       func() (root string, stale bool, err error)
	LastSuccessfulSource func(appName string) (source SyncedSource, found bool, err error)
	RenderRevision       func(repoURL, revision, sourcePath string) ([]byte, []render.Resource, error)
	WorkDir              string
}

type SyncedSource struct {
	RepoURL  string
	Revision string
	Path     string
}

const (
	exitPass      = 0
	exitViolation = 1
	exitError     = 2
)

// Run executes the pipeline and returns the process exit code.
func Run(d Deps, stdout, stderr io.Writer) int {
	// The CMP server invokes the plugin with the working directory already set
	// to the app source path, so the build target is always ".". Passing
	// ARGOCD_APP_SOURCE_PATH (repo-root-relative) here would double the path.
	raw, currentResources, err := render.Build(".", d.Kustomize)
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

	if sub := d.Getenv("GUARD_POLICY_PATH"); sub != "" {
		cleaned := filepath.Clean(sub)
		if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			fmt.Fprintf(stderr, "argo-guard: GUARD_POLICY_PATH %q must be a relative path inside the policy repo\n", sub)
			return exitError
		}
		policyRoot = filepath.Join(policyRoot, cleaned)
	}

	registry, err := bundles.Load(filepath.Join(policyRoot, "guard.yaml"))
	if err != nil {
		fmt.Fprintf(stderr, "argo-guard: %v\n", err)
		return exitError
	}
	manifestBundles := registry.SelectMode(ctx, bundles.ModeManifest)
	if len(manifestBundles) == 0 {
		fmt.Fprintf(stderr, "argo-guard: no policy bundles matched (guard.yaml must include a global match:{} baseline)\n")
		return exitError
	}

	res, err := evaluate.Run(raw, ctx, policyRoot, manifestBundles, d.WorkDir, d.Conftest)
	if err != nil {
		fmt.Fprintf(stderr, "argo-guard: %v\n", err)
		return exitError
	}

	if len(res.Denies) > 0 {
		emit.Report(stderr, res, stale)
		return exitViolation
	}

	transitionBundles := registry.SelectMode(ctx, bundles.ModeTransition)
	if len(transitionBundles) > 0 {
		transitionResult, err := evaluateTransitions(d, ctx, policyRoot, transitionBundles, currentResources)
		if err != nil {
			fmt.Fprintf(stderr, "argo-guard: %v\n", err)
			return exitError
		}
		res.Denies = append(res.Denies, transitionResult.Denies...)
		res.Warns = append(res.Warns, transitionResult.Warns...)
		if len(res.Denies) > 0 {
			emit.Report(stderr, res, stale)
			return exitViolation
		}
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

func evaluateTransitions(d Deps, ctx trust.Context, policyRoot string, bundleDirs []string, currentResources []render.Resource) (evaluate.Result, error) {
	if d.LastSuccessfulSource == nil || d.RenderRevision == nil {
		return evaluate.Result{}, fmt.Errorf("transition policy dependencies are not configured (fail-closed)")
	}
	appName := d.Getenv("ARGOCD_APP_NAME")
	currentRevision := d.Getenv("ARGOCD_APP_REVISION")
	if appName == "" || currentRevision == "" {
		return evaluate.Result{}, fmt.Errorf("ARGOCD_APP_NAME and ARGOCD_APP_REVISION are required for transition policies (fail-closed)")
	}

	previous, found, err := d.LastSuccessfulSource(appName)
	if err != nil {
		return evaluate.Result{}, fmt.Errorf("resolve last successful sync revision: %w", err)
	}
	var previousResources []render.Resource
	if found && previous.Revision != currentRevision {
		previousRepo := previous.RepoURL
		if previousRepo == "" {
			previousRepo = ctx.Repo
		}
		_, previousResources, err = d.RenderRevision(previousRepo, previous.Revision, previous.Path)
		if err != nil {
			return evaluate.Result{}, fmt.Errorf("render last successful revision %s: %w", previous.Revision, err)
		}
	} else if found {
		previousResources = currentResources
	}

	changes, err := transition.Diff(previousResources, currentResources)
	if err != nil {
		return evaluate.Result{}, fmt.Errorf("build manifest transition: %w", err)
	}
	if len(changes) == 0 {
		return evaluate.Result{}, nil
	}
	input, err := transition.Encode(changes)
	if err != nil {
		return evaluate.Result{}, err
	}
	return evaluate.Run(input, ctx, policyRoot, bundleDirs, d.WorkDir, d.Conftest)
}
