// Package generate orchestrates the full render→validate→emit pipeline that
// backs the `argo-guard generate` CMP command. It is fail-closed: any error
// returns exit code 2 and never emits manifests.
package generate

import (
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"github.com/ahmetozer/argo-guard/internal/bundles"
	"github.com/ahmetozer/argo-guard/internal/emit"
	"github.com/ahmetozer/argo-guard/internal/evaluate"
	"github.com/ahmetozer/argo-guard/internal/render"
	"github.com/ahmetozer/argo-guard/internal/repourl"
	"github.com/ahmetozer/argo-guard/internal/transition"
	"github.com/ahmetozer/argo-guard/internal/trust"
)

type Deps struct {
	Getenv         func(string) string
	Kustomize      render.KustomizeFunc
	Conftest       evaluate.ConftestFunc
	EnsurePolicies func() (root string, stale bool, err error)
	// Transition is nil when the host cannot evaluate transition bundles (no
	// Argo CD API access, no application Git credentials). A matched
	// transition bundle then fails closed rather than being silently skipped.
	Transition *TransitionDeps
	// WorkDir is a scratch root. Callers own its removal; evaluate.Run carves
	// an isolated --data subdirectory out of it per phase, so unrelated
	// scratch state here is never visible to policies.
	WorkDir string
}

// TransitionDeps are needed together or not at all — resolving the last
// successful source is useless without the ability to render it — so they are
// grouped rather than checked independently at the point of use.
type TransitionDeps struct {
	LastSuccessfulSource func(appName string) (source SyncedSource, found bool, err error)
	RenderRevision       func(repoURL, revision, sourcePath string) ([]byte, []render.Resource, error)
}

func (t *TransitionDeps) complete() bool {
	return t != nil && t.LastSuccessfulSource != nil && t.RenderRevision != nil
}

type SyncedSource struct {
	RepoURL     string
	Revision    string
	Path        string
	Destination Destination
}

type Destination struct {
	Server    string `json:"server"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
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

	res, err := evaluate.Run(raw, ctx, policyRoot, manifestBundles, d.WorkDir, nil, d.Conftest)
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
	if !d.Transition.complete() {
		return evaluate.Result{}, fmt.Errorf("a transition bundle matched but this build cannot evaluate transitions (fail-closed)")
	}
	app := trust.AppRefFromEnv(d.Getenv)
	if err := app.Validate(); err != nil {
		return evaluate.Result{}, fmt.Errorf("transition policies require the Application identity Argo injects: %w (fail-closed)", err)
	}

	previous, found, err := d.Transition.LastSuccessfulSource(app.Name)
	if err != nil {
		return evaluate.Result{}, fmt.Errorf("resolve previous desired-state revision from the last successful sync: %w", err)
	}
	var previousResources []render.Resource
	if found {
		sameRepo, err := repourl.Equal(ctx.Repo, previous.RepoURL)
		if err != nil {
			return evaluate.Result{}, fmt.Errorf("validate desired-state repository identity: %w (fail-closed)", err)
		}
		if !sameRepo {
			return evaluate.Result{}, fmt.Errorf("previous desired-state repository differs from the current repository; current repository credentials will not be forwarded (fail-closed)")
		}
		if previous.Revision == app.Revision && sameSourcePath(previous.Path, app.SourcePath) {
			previousResources = currentResources
		} else {
			_, previousResources, err = d.Transition.RenderRevision(previous.RepoURL, previous.Revision, previous.Path)
			if err != nil {
				return evaluate.Result{}, fmt.Errorf("render previous desired state at revision %s: %w", previous.Revision, err)
			}
		}
	}

	changes, err := transition.Diff(previousResources, currentResources)
	if err != nil {
		return evaluate.Result{}, fmt.Errorf("build desired-state manifest transition: %w", err)
	}
	if len(changes) == 0 {
		return evaluate.Result{}, nil
	}
	input, err := transition.Encode(changes)
	if err != nil {
		return evaluate.Result{}, err
	}
	runtimeData := map[string]any{
		"transition": map[string]any{
			"applicationName":  app.Name,
			"destination":      previous.Destination,
			"previousRevision": previous.Revision,
			"currentRevision":  app.Revision,
			"changes":          transition.Summaries(changes),
			"beforeResources":  resourceDocuments(previousResources),
			"afterResources":   resourceDocuments(currentResources),
		},
	}
	return evaluate.Run(input, ctx, policyRoot, bundleDirs, d.WorkDir, runtimeData, d.Conftest)
}

func sameSourcePath(left, right string) bool {
	clean := func(value string) string {
		if value == "" {
			return "."
		}
		return path.Clean(value)
	}
	return clean(left) == clean(right)
}

func resourceDocuments(resources []render.Resource) []map[string]any {
	documents := make([]map[string]any, 0, len(resources))
	for _, resource := range resources {
		documents = append(documents, resource.Doc)
	}
	return documents
}
