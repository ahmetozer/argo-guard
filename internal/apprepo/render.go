// Package apprepo renders an application at a specific Git revision.
package apprepo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ahmetozer/argo-guard/internal/render"
)

// GitFunc runs a git command in workdir.
type GitFunc func(workdir string, args ...string) error

// RenderRevision fetches one resolved Git revision into a temporary checkout
// and renders the application's repo-relative source path.
func RenderRevision(repoURL, revision, sourcePath, workDir string, git GitFunc, kustomize render.KustomizeFunc) ([]byte, []render.Resource, error) {
	if repoURL == "" {
		return nil, nil, fmt.Errorf("application repo URL is required")
	}
	if revision == "" || strings.HasPrefix(revision, "-") || strings.ContainsAny(revision, " \t\r\n") {
		return nil, nil, fmt.Errorf("invalid application revision %q", revision)
	}
	cleanPath := filepath.Clean(sourcePath)
	if sourcePath == "" {
		cleanPath = "."
	}
	if filepath.IsAbs(cleanPath) || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
		return nil, nil, fmt.Errorf("application source path %q must stay inside the repo", sourcePath)
	}

	checkout, err := os.MkdirTemp(workDir, "app-revision-")
	if err != nil {
		return nil, nil, fmt.Errorf("create revision checkout: %w", err)
	}
	defer os.RemoveAll(checkout)

	if err := git("", "init", "--quiet", checkout); err != nil {
		return nil, nil, fmt.Errorf("initialize application checkout: %w", err)
	}
	if err := git(checkout, "remote", "add", "origin", repoURL); err != nil {
		return nil, nil, fmt.Errorf("configure application checkout: %w", err)
	}
	// Fetching a bare commit SHA requires the server to advertise
	// uploadpack.allowAnySHA1InWant (or allowReachableSHA1InWant). GitHub and
	// GitLab do; some self-hosted servers do not, and their refusal is
	// indistinguishable from a missing revision without the hint.
	if err := git(checkout, "fetch", "--depth", "1", "origin", revision); err != nil {
		return nil, nil, fmt.Errorf("fetch previous desired-state revision %s: %w "+
			"(the revision may be gone, or the Git server may not allow fetching a commit "+
			"by SHA — it needs uploadpack.allowAnySHA1InWant or allowReachableSHA1InWant)",
			revision, err)
	}
	if err := git(checkout, "checkout", "--quiet", "--detach", "FETCH_HEAD"); err != nil {
		return nil, nil, fmt.Errorf("checkout previous desired-state revision %s: %w", revision, err)
	}

	return render.Build(filepath.Join(checkout, cleanPath), kustomize)
}
