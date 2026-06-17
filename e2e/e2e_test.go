//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGuardRepo builds the binary once and runs it against a fixture app with
// the given repo URL, returning exit code, stdout, and stderr.
func runGuardRepo(t *testing.T, appPath, repoURL string) (int, string, string) {
	t.Helper()
	repoRoot, _ := filepath.Abs("..")
	bin := filepath.Join(t.TempDir(), "argo-guard")
	build := exec.Command("go", "build", "-o", bin, "./cmd/argo-guard")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cmd := exec.Command(bin, "generate")
	cmd.Env = append(os.Environ(),
		"ARGOCD_APP_SOURCE_PATH="+filepath.Join(repoRoot, appPath),
		"ARGOCD_APP_SOURCE_REPO_URL="+repoURL,
		"ARGOCD_APP_PROJECT_NAME=team-a",
		// Point the cache directly at the example policies (skip git clone) by
		// pre-seeding GUARD_POLICY_CACHE and a far-future TTL.
		"GUARD_POLICY_CACHE="+filepath.Join(repoRoot, "examples/policies"),
		"GUARD_POLICY_TTL=8760h",
		// Pin local-cache mode; immune to ambient GUARD_POLICY_REPO.
		"GUARD_POLICY_REPO=",
	)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run: %v", err)
	}
	return code, out.String(), errb.String()
}

// runGuard delegates to runGuardRepo with the default team-a repo URL.
func runGuard(t *testing.T, appPath string) (int, string, string) {
	t.Helper()
	return runGuardRepo(t, appPath, "https://git.corp/team-a/app.git")
}

func TestE2ECleanAppPasses(t *testing.T) {
	code, stdout, stderr := runGuard(t, "e2e/fixtures/clean-app")
	if code != 0 {
		t.Fatalf("expected pass, got %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "kind: Deployment") {
		t.Fatalf("expected manifests on stdout:\n%s", stdout)
	}
}

func TestE2EBadAppDenied(t *testing.T) {
	code, stdout, stderr := runGuard(t, "e2e/fixtures/bad-app")
	if code != 1 {
		t.Fatalf("expected violation exit 1, got %d\n%s", code, stderr)
	}
	if stdout != "" {
		t.Fatal("must not emit manifests on violation")
	}
	if !strings.Contains(stderr, "LoadBalancer") {
		t.Fatalf("expected LoadBalancer denial:\n%s", stderr)
	}
}

func TestE2ETrustedRepoClusterRoleAllowed(t *testing.T) {
	code, stdout, stderr := runGuardRepo(t, "e2e/fixtures/trusted-rbac-app", "https://git.corp/infra/platform.git")
	if code != 0 {
		t.Fatalf("expected pass for trusted repo, got %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "kind: ClusterRole") {
		t.Fatalf("expected ClusterRole on stdout:\n%s", stdout)
	}
}

func TestE2EUntrustedRepoClusterRoleDenied(t *testing.T) {
	code, stdout, stderr := runGuardRepo(t, "e2e/fixtures/untrusted-rbac-app", "https://git.corp/team-a/app.git")
	if code != 1 {
		t.Fatalf("expected violation exit 1 for untrusted repo, got %d\nstderr: %s", code, stderr)
	}
	if stdout != "" {
		t.Fatal("must not emit manifests on violation")
	}
	if !strings.Contains(stderr, "trusted infra repos") {
		t.Fatalf("expected 'trusted infra repos' in stderr:\n%s", stderr)
	}
}
