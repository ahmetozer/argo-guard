package generate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmetozer/argo-guard/internal/render"
)

// fakePolicyRoot writes a guard.yaml so Select returns the global bundle.
func fakePolicyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	guard := "bundles:\n  - dir: global\n    match: {}\n"
	if err := os.WriteFile(filepath.Join(root, "guard.yaml"), []byte(guard), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func fakeTransitionPolicyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	guard := `bundles:
  - dir: global
    match: {}
  - dir: production-transitions
    mode: transition
    match:
      project: production
`
	if err := os.WriteFile(filepath.Join(root, "guard.yaml"), []byte(guard), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"global", "production-transitions"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func parsedResources(t *testing.T, manifest string) []render.Resource {
	t.Helper()
	_, resources, err := render.Build(".", func(string) ([]byte, error) { return []byte(manifest), nil })
	if err != nil {
		t.Fatal(err)
	}
	return resources
}

func baseDeps(t *testing.T, conftestOut string, conftestErr error) (Deps, string) {
	root := fakePolicyRoot(t)
	d := Deps{
		Getenv:         func(k string) string { return map[string]string{"ARGOCD_APP_SOURCE_PATH": "app/"}[k] },
		Kustomize:      func(string) ([]byte, error) { return []byte("kind: Service\nmetadata:\n  name: web\n"), nil },
		Conftest:       func([]string, []byte) ([]byte, error) { return []byte(conftestOut), conftestErr },
		EnsurePolicies: func() (string, bool, error) { return root, false, nil },
		WorkDir:        t.TempDir(),
	}
	return d, root
}

func TestRunCleanEmitsManifests(t *testing.T) {
	d, _ := baseDeps(t, `[{"filename":"-","failures":[],"warnings":[]}]`, nil)
	var out, errb bytes.Buffer
	code := Run(d, &out, &errb)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if out.String() == "" {
		t.Fatal("expected manifests on stdout")
	}
}

func TestRunKustomizeBuildsCwd(t *testing.T) {
	// The CMP server runs the plugin with the working directory already set to
	// the app source path; passing ARGOCD_APP_SOURCE_PATH to kustomize again
	// doubles the path (e.g. .../agent-controller/ops/agent-controller/ops).
	d, _ := baseDeps(t, `[{"filename":"-","failures":[],"warnings":[]}]`, nil)
	var got string
	d.Kustomize = func(path string) ([]byte, error) {
		got = path
		return []byte("kind: Service\nmetadata:\n  name: web\n"), nil
	}
	var out, errb bytes.Buffer
	if code := Run(d, &out, &errb); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if got != "." {
		t.Fatalf("kustomize build target = %q, want %q", got, ".")
	}
}

func TestRunViolationExits1NoManifests(t *testing.T) {
	d, _ := baseDeps(t, `[{"filename":"-","failures":[{"msg":"no LoadBalancer"}],"warnings":[]}]`, nil)
	var out, errb bytes.Buffer
	code := Run(d, &out, &errb)
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
	if out.Len() != 0 {
		t.Fatal("must NOT emit manifests on violation")
	}
}

func TestRunInternalErrorExits2(t *testing.T) {
	d, _ := baseDeps(t, "", errors.New("conftest exploded"))
	var out, errb bytes.Buffer
	code := Run(d, &out, &errb)
	if code != 2 {
		t.Fatalf("want exit 2 (fail-closed), got %d", code)
	}
	if out.Len() != 0 {
		t.Fatal("must NOT emit manifests on internal error")
	}
}

func TestRunKustomizeErrorExits2(t *testing.T) {
	d, _ := baseDeps(t, `[]`, nil)
	d.Kustomize = func(string) ([]byte, error) { return nil, errors.New("bad kustomization") }
	var out, errb bytes.Buffer
	if code := Run(d, &out, &errb); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
	_ = render.Resource{} // ensure render import used
}

// fakeNonMatchingPolicyRoot writes a guard.yaml whose only bundle requires a
// specific namespace, so it never matches the default (empty) context.
func fakeNonMatchingPolicyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	guard := "bundles:\n  - dir: global\n    match:\n      namespace:\n        equals: other\n"
	if err := os.WriteFile(filepath.Join(root, "guard.yaml"), []byte(guard), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunPolicyPathSelectsSubdir(t *testing.T) {
	// guard.yaml lives under policies/prod inside the repo, not at the root.
	root := t.TempDir()
	sub := filepath.Join(root, "policies", "prod")
	if err := os.MkdirAll(filepath.Join(sub, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	guard := "bundles:\n  - dir: global\n    match: {}\n"
	if err := os.WriteFile(filepath.Join(sub, "guard.yaml"), []byte(guard), 0o644); err != nil {
		t.Fatal(err)
	}
	d := Deps{
		Getenv: func(k string) string {
			return map[string]string{
				"ARGOCD_APP_SOURCE_PATH": "app/",
				"GUARD_POLICY_PATH":      "policies/prod",
			}[k]
		},
		Kustomize: func(string) ([]byte, error) { return []byte("kind: Service\nmetadata:\n  name: web\n"), nil },
		Conftest: func([]string, []byte) ([]byte, error) {
			return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
		},
		EnsurePolicies: func() (string, bool, error) { return root, false, nil },
		WorkDir:        t.TempDir(),
	}
	var out, errb bytes.Buffer
	if code := Run(d, &out, &errb); code != 0 {
		t.Fatalf("want 0, got %d; stderr=%s", code, errb.String())
	}
	if out.String() == "" {
		t.Fatal("expected manifests on stdout")
	}
}

func TestRunPolicyPathEscapeExits2(t *testing.T) {
	for _, p := range []string{"../outside", "/etc"} {
		d, _ := baseDeps(t, `[]`, nil)
		d.Getenv = func(k string) string {
			return map[string]string{
				"ARGOCD_APP_SOURCE_PATH": "app/",
				"GUARD_POLICY_PATH":      p,
			}[k]
		}
		var out, errb bytes.Buffer
		if code := Run(d, &out, &errb); code != 2 {
			t.Fatalf("GUARD_POLICY_PATH=%q: want exit 2 (fail-closed), got %d", p, code)
		}
		if out.Len() != 0 {
			t.Fatalf("GUARD_POLICY_PATH=%q: must NOT emit manifests", p)
		}
	}
}

func TestRunNoBundlesMatchedExits2(t *testing.T) {
	root := fakeNonMatchingPolicyRoot(t)
	d := Deps{
		Getenv:         func(k string) string { return map[string]string{"ARGOCD_APP_SOURCE_PATH": "app/"}[k] },
		Kustomize:      func(string) ([]byte, error) { return []byte("kind: Service\nmetadata:\n  name: web\n"), nil },
		Conftest:       func([]string, []byte) ([]byte, error) { return []byte(`[]`), nil },
		EnsurePolicies: func() (string, bool, error) { return root, false, nil },
		WorkDir:        t.TempDir(),
	}
	var out, errb bytes.Buffer
	code := Run(d, &out, &errb)
	if code != 2 {
		t.Fatalf("want exit 2 (no bundles matched), got %d; stderr=%s", code, errb.String())
	}
	if out.Len() != 0 {
		t.Fatal("must NOT emit manifests when no bundles matched")
	}
	if !strings.Contains(errb.String(), "no policy bundles matched") {
		t.Fatalf("expected clear error message, got: %s", errb.String())
	}
}

func transitionDeps(t *testing.T, currentManifest string) Deps {
	t.Helper()
	root := fakeTransitionPolicyRoot(t)
	env := map[string]string{
		"ARGOCD_APP_NAME":            "payments",
		"ARGOCD_APP_REVISION":        "bbbb",
		"ARGOCD_APP_SOURCE_PATH":     "deploy/prod",
		"ARGOCD_APP_SOURCE_REPO_URL": "https://git.example/payments.git",
		"ARGOCD_APP_PROJECT_NAME":    "production",
	}
	return Deps{
		Getenv:         func(k string) string { return env[k] },
		Kustomize:      func(string) ([]byte, error) { return []byte(currentManifest), nil },
		EnsurePolicies: func() (string, bool, error) { return root, false, nil },
		WorkDir:        t.TempDir(),
	}
}

func TestRunTransitionViolationBlocksManifests(t *testing.T) {
	current := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 0}\n"
	previous := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 2}\n"
	d := transitionDeps(t, current)
	d.LastSuccessfulSource = func(appName string) (SyncedSource, bool, error) {
		if appName != "payments" {
			t.Fatalf("appName=%q", appName)
		}
		return SyncedSource{RepoURL: "https://git.example/previous.git", Revision: "aaaa", Path: "old/prod"}, true, nil
	}
	d.RenderRevision = func(repoURL, revision, sourcePath string) ([]byte, []render.Resource, error) {
		if repoURL != "https://git.example/previous.git" || revision != "aaaa" || sourcePath != "old/prod" {
			t.Fatalf("repo=%q revision=%q path=%q", repoURL, revision, sourcePath)
		}
		return []byte(previous), parsedResources(t, previous), nil
	}
	conftestCalls := 0
	d.Conftest = func(_ []string, stdin []byte) ([]byte, error) {
		conftestCalls++
		if conftestCalls == 1 {
			return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
		}
		if !strings.Contains(string(stdin), "kind: ManifestChange") || !strings.Contains(string(stdin), "operation: Update") {
			t.Fatalf("transition input:\n%s", stdin)
		}
		return []byte(`[{"filename":"-","failures":[{"msg":"cannot scale to zero"}],"warnings":[]}]`), nil
	}

	var out, errb bytes.Buffer
	if code := Run(d, &out, &errb); code != exitViolation {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if out.Len() != 0 || !strings.Contains(errb.String(), "cannot scale to zero") {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errb.String())
	}
}

func TestRunTransitionNoHistoryEvaluatesCreates(t *testing.T) {
	current := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 0}\n"
	d := transitionDeps(t, current)
	d.LastSuccessfulSource = func(string) (SyncedSource, bool, error) { return SyncedSource{}, false, nil }
	d.RenderRevision = func(string, string, string) ([]byte, []render.Resource, error) {
		t.Fatal("new application must not render a previous revision")
		return nil, nil, nil
	}
	conftestCalls := 0
	d.Conftest = func(_ []string, stdin []byte) ([]byte, error) {
		conftestCalls++
		if conftestCalls == 2 && !strings.Contains(string(stdin), "operation: Create") {
			t.Fatalf("transition input:\n%s", stdin)
		}
		return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
	}
	var out, errb bytes.Buffer
	if code := Run(d, &out, &errb); code != exitPass {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if conftestCalls != 2 {
		t.Fatalf("conftest calls=%d", conftestCalls)
	}
}

func TestRunTransitionSameRevisionSkipsPreviousRender(t *testing.T) {
	current := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 0}\n"
	d := transitionDeps(t, current)
	d.LastSuccessfulSource = func(string) (SyncedSource, bool, error) { return SyncedSource{Revision: "bbbb"}, true, nil }
	d.RenderRevision = func(string, string, string) ([]byte, []render.Resource, error) {
		t.Fatal("same revision must not be fetched")
		return nil, nil, nil
	}
	conftestCalls := 0
	d.Conftest = func(_ []string, _ []byte) ([]byte, error) {
		conftestCalls++
		return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
	}
	var out, errb bytes.Buffer
	if code := Run(d, &out, &errb); code != exitPass {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if conftestCalls != 1 {
		t.Fatalf("conftest calls=%d, transition evaluation should be skipped", conftestCalls)
	}
}

func TestRunTransitionResolverErrorFailsClosed(t *testing.T) {
	current := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 0}\n"
	d := transitionDeps(t, current)
	d.LastSuccessfulSource = func(string) (SyncedSource, bool, error) { return SyncedSource{}, false, errors.New("forbidden") }
	d.RenderRevision = func(string, string, string) ([]byte, []render.Resource, error) { return nil, nil, nil }
	d.Conftest = func(_ []string, _ []byte) ([]byte, error) {
		return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
	}
	var out, errb bytes.Buffer
	if code := Run(d, &out, &errb); code != exitError {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if out.Len() != 0 || !strings.Contains(errb.String(), "resolve last successful sync revision") {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errb.String())
	}
}
