//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGuardEnv builds the binary once and runs it against a fixture app with
// the given repo URL and extra env, returning exit code, stdout, and stderr.
func runGuardEnv(t *testing.T, appPath, repoURL string, extraEnv ...string) (int, string, string) {
	t.Helper()
	repoRoot, _ := filepath.Abs("..")
	bin := filepath.Join(t.TempDir(), "argo-guard")
	build := exec.Command("go", "build", "-o", bin, "./cmd/argo-guard")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cmd := exec.Command(bin, "generate")
	// Mimic the CMP server contract: the plugin runs with the working directory
	// set to the app source path; ARGOCD_APP_SOURCE_PATH is repo-relative.
	cmd.Dir = filepath.Join(repoRoot, appPath)
	cmd.Env = append(os.Environ(),
		"ARGOCD_APP_SOURCE_PATH="+appPath,
		"ARGOCD_APP_SOURCE_REPO_URL="+repoURL,
		"ARGOCD_APP_PROJECT_NAME=team-a",
		// Pin local-cache mode; immune to ambient GUARD_POLICY_REPO.
		"GUARD_POLICY_REPO=",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
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

// runGuardRepo runs against the pre-seeded examples/policies directory
// (plain-tree local mode).
func runGuardRepo(t *testing.T, appPath, repoURL string) (int, string, string) {
	t.Helper()
	repoRoot, _ := filepath.Abs("..")
	return runGuardEnv(t, appPath, repoURL,
		"GUARD_POLICY_CACHE="+filepath.Join(repoRoot, "examples/policies"),
		"GUARD_POLICY_TTL=8760h",
	)
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

// flattenPolicies simulates a ConfigMap volume mount of examples/policies:
// flattened key names, a timestamped data dir, a ..data symlink, and one
// top-level symlink per key — exactly the shape kubelet produces.
func flattenPolicies(t *testing.T, include map[string]string) string {
	t.Helper()
	repoRoot, _ := filepath.Abs("..")
	src := filepath.Join(repoRoot, "examples/policies")
	mount := t.TempDir()
	dataDir := "..2026_07_21_00_00_00.0000000001"
	if err := os.MkdirAll(filepath.Join(mount, dataDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for key, rel := range include {
		data, err := os.ReadFile(filepath.Join(src, rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mount, dataDir, key), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("..data", key), filepath.Join(mount, key)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(dataDir, filepath.Join(mount, "..data")); err != nil {
		t.Fatal(err)
	}
	return mount
}

func fullFlatPolicies(t *testing.T) string {
	return flattenPolicies(t, map[string]string{
		"guard.yaml":                "guard.yaml",
		"global__restrictions.rego": "global/restrictions.rego",
		"global__data.json":         "global/data.json",
	})
}

func TestE2EFlatPolicyCleanAppPasses(t *testing.T) {
	code, stdout, stderr := runGuardEnv(t, "e2e/fixtures/clean-app", "https://git.corp/team-a/app.git",
		"GUARD_POLICY_CACHE="+fullFlatPolicies(t), "GUARD_POLICY_FLAT=true")
	if code != 0 {
		t.Fatalf("expected pass, got %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "kind: Deployment") {
		t.Fatalf("expected manifests on stdout:\n%s", stdout)
	}
}

func TestE2EFlatPolicyBadAppDenied(t *testing.T) {
	code, stdout, stderr := runGuardEnv(t, "e2e/fixtures/bad-app", "https://git.corp/team-a/app.git",
		"GUARD_POLICY_CACHE="+fullFlatPolicies(t), "GUARD_POLICY_FLAT=true")
	if code != 1 {
		t.Fatalf("expected violation exit 1, got %d\n%s", code, stderr)
	}
	if stdout != "" {
		t.Fatal("must not emit manifests on violation")
	}
	if !strings.Contains(stderr, "LoadBalancer") {
		t.Fatalf("expected LoadBalancer denial (policies loaded from flat dir):\n%s", stderr)
	}
}

func TestE2EFlatPolicyMissingGuardYamlFailsClosed(t *testing.T) {
	mount := flattenPolicies(t, map[string]string{
		"global__restrictions.rego": "global/restrictions.rego",
		"global__data.json":         "global/data.json",
	})
	code, stdout, _ := runGuardEnv(t, "e2e/fixtures/clean-app", "https://git.corp/team-a/app.git",
		"GUARD_POLICY_CACHE="+mount, "GUARD_POLICY_FLAT=true")
	if code != 2 {
		t.Fatalf("expected fail-closed exit 2 without guard.yaml, got %d", code)
	}
	if stdout != "" {
		t.Fatal("must not emit manifests")
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
