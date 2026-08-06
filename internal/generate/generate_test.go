package generate

import (
	"bytes"
	"encoding/json"
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

// policyDataDir returns the leading --data value conftest was invoked with:
// the isolated directory evaluate builds for a single phase.
func policyDataDir(t *testing.T, args []string) string {
	t.Helper()
	for i, a := range args {
		if a == "--data" && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("no --data flag in conftest args: %v", args)
	return ""
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
		Transition:     &TransitionDeps{},
		WorkDir:        t.TempDir(),
	}
}

func TestRunTransitionViolationBlocksManifests(t *testing.T) {
	current := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 0}\n"
	previous := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 2}\n"
	d := transitionDeps(t, current)
	d.Transition.LastSuccessfulSource = func(appName string) (SyncedSource, bool, error) {
		if appName != "payments" {
			t.Fatalf("appName=%q", appName)
		}
		return SyncedSource{RepoURL: "https://GIT.EXAMPLE/payments", Revision: "aaaa", Path: "old/prod"}, true, nil
	}
	d.Transition.RenderRevision = func(repoURL, revision, sourcePath string) ([]byte, []render.Resource, error) {
		if repoURL != "https://GIT.EXAMPLE/payments" || revision != "aaaa" || sourcePath != "old/prod" {
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
	d.Transition.LastSuccessfulSource = func(string) (SyncedSource, bool, error) { return SyncedSource{}, false, nil }
	d.Transition.RenderRevision = func(string, string, string) ([]byte, []render.Resource, error) {
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

func TestRunTransitionForwardOnlyRevertCommitEvaluatesPreviousToNewSHA(t *testing.T) {
	currentAtC := "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: baseline}\n"
	previousAtB := currentAtC + "---\napiVersion: v1\nkind: Service\nmetadata: {name: added-at-b}\nspec: {ports: [{port: 80}]}\n"
	d := transitionDeps(t, currentAtC)
	getenv := d.Getenv
	d.Getenv = func(key string) string {
		if key == "ARGOCD_APP_REVISION" {
			return "commit-c"
		}
		return getenv(key)
	}
	d.Transition.LastSuccessfulSource = func(string) (SyncedSource, bool, error) {
		return SyncedSource{RepoURL: "https://git.example/payments.git", Revision: "commit-b", Path: "deploy/prod"}, true, nil
	}
	d.Transition.RenderRevision = func(repoURL, revision, sourcePath string) ([]byte, []render.Resource, error) {
		if revision != "commit-b" {
			t.Fatalf("previous revision=%q", revision)
		}
		return []byte(previousAtB), parsedResources(t, previousAtB), nil
	}
	conftestCalls := 0
	d.Conftest = func(_ []string, stdin []byte) ([]byte, error) {
		conftestCalls++
		if conftestCalls == 2 {
			input := string(stdin)
			if !strings.Contains(input, "operation: Delete") || !strings.Contains(input, "name: added-at-b") {
				t.Fatalf("B -> new revert commit C transition input:\n%s", input)
			}
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

func TestRunTransitionExposesCompleteDesiredStates(t *testing.T) {
	current := `apiVersion: apps/v1
kind: Deployment
metadata: {name: web, namespace: payments}
spec:
  template:
    spec:
      serviceAccountName: payment-api
      containers: [{name: web, image: example/web}]
`
	previous := `apiVersion: v1
kind: ServiceAccount
metadata: {name: payment-api, namespace: payments}
---
` + current

	d := transitionDeps(t, current)
	d.Transition.LastSuccessfulSource = func(string) (SyncedSource, bool, error) {
		return SyncedSource{
			RepoURL: "https://git.example/payments.git", Revision: "aaaa", Path: "deploy/prod",
			Destination: Destination{Server: "https://prod.example", Namespace: "payments"},
		}, true, nil
	}
	d.Transition.RenderRevision = func(string, string, string) ([]byte, []render.Resource, error) {
		return []byte(previous), parsedResources(t, previous), nil
	}
	conftestCalls := 0
	d.Conftest = func(args []string, stdin []byte) ([]byte, error) {
		conftestCalls++
		if conftestCalls == 1 {
			return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
		}
		if !strings.Contains(string(stdin), "operation: Delete") || !strings.Contains(string(stdin), "kind: ServiceAccount") {
			t.Fatalf("transition input:\n%s", stdin)
		}
		// Runtime data lives in the isolated --data dir evaluate builds per
		// phase, not in the scratch root.
		raw, err := os.ReadFile(filepath.Join(policyDataDir(t, args), "runtime-data.json"))
		if err != nil {
			t.Fatalf("read runtime data: %v", err)
		}
		var data struct {
			Transition struct {
				ApplicationName  string           `json:"applicationName"`
				Destination      Destination      `json:"destination"`
				PreviousRevision string           `json:"previousRevision"`
				CurrentRevision  string           `json:"currentRevision"`
				Changes          []map[string]any `json:"changes"`
				BeforeResources  []map[string]any `json:"beforeResources"`
				AfterResources   []map[string]any `json:"afterResources"`
			} `json:"transition"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		if data.Transition.ApplicationName != "payments" {
			t.Fatalf("applicationName=%q", data.Transition.ApplicationName)
		}
		if data.Transition.Destination.Server != "https://prod.example" || data.Transition.Destination.Namespace != "payments" {
			t.Fatalf("destination=%+v", data.Transition.Destination)
		}
		if data.Transition.PreviousRevision != "aaaa" || data.Transition.CurrentRevision != "bbbb" {
			t.Fatalf("revisions=%+v", data.Transition)
		}
		if len(data.Transition.Changes) != 1 || len(data.Transition.BeforeResources) != 2 || len(data.Transition.AfterResources) != 1 {
			t.Fatalf("transition runtime data=%s", raw)
		}
		if data.Transition.AfterResources[0]["kind"] != "Deployment" {
			t.Fatalf("unchanged Deployment missing from afterResources: %s", raw)
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
	d.Transition.LastSuccessfulSource = func(string) (SyncedSource, bool, error) {
		return SyncedSource{RepoURL: "https://git.example/payments.git", Revision: "bbbb", Path: "deploy/prod"}, true, nil
	}
	d.Transition.RenderRevision = func(string, string, string) ([]byte, []render.Resource, error) {
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

func TestRunTransitionSameRevisionDifferentPathRendersPreviousDesiredState(t *testing.T) {
	current := "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: current}\n"
	previous := "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: previous}\n"
	d := transitionDeps(t, current)
	d.Transition.LastSuccessfulSource = func(string) (SyncedSource, bool, error) {
		return SyncedSource{RepoURL: "https://git.example/payments.git", Revision: "bbbb", Path: "deploy/old"}, true, nil
	}
	rendered := false
	d.Transition.RenderRevision = func(repoURL, revision, sourcePath string) ([]byte, []render.Resource, error) {
		rendered = true
		if repoURL != "https://git.example/payments.git" || revision != "bbbb" || sourcePath != "deploy/old" {
			t.Fatalf("repo=%q revision=%q path=%q", repoURL, revision, sourcePath)
		}
		return []byte(previous), parsedResources(t, previous), nil
	}
	d.Conftest = func(_ []string, _ []byte) ([]byte, error) {
		return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
	}
	var out, errb bytes.Buffer
	if code := Run(d, &out, &errb); code != exitPass {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if !rendered {
		t.Fatal("same revision at a different source path must render the previous desired state")
	}
}

func TestRunTransitionDifferentPreviousRepositoryFailsClosed(t *testing.T) {
	secret := "must-not-leak"
	d := transitionDeps(t, "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: current}\n")
	d.Transition.LastSuccessfulSource = func(string) (SyncedSource, bool, error) {
		return SyncedSource{RepoURL: "https://user:" + secret + "@other.example/payments.git", Revision: "aaaa", Path: "deploy/prod"}, true, nil
	}
	d.Transition.RenderRevision = func(string, string, string) ([]byte, []render.Resource, error) {
		t.Fatal("different repository credentials must never reach git")
		return nil, nil, nil
	}
	d.Conftest = func(_ []string, _ []byte) ([]byte, error) {
		return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
	}
	var out, errb bytes.Buffer
	if code := Run(d, &out, &errb); code != exitError {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if out.Len() != 0 || strings.Contains(errb.String(), secret) {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "fail-closed") {
		t.Fatalf("expected fail-closed repository error, got %q", errb.String())
	}
}

func TestRunTransitionResolverErrorFailsClosed(t *testing.T) {
	current := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 0}\n"
	d := transitionDeps(t, current)
	d.Transition.LastSuccessfulSource = func(string) (SyncedSource, bool, error) { return SyncedSource{}, false, errors.New("forbidden") }
	d.Transition.RenderRevision = func(string, string, string) ([]byte, []render.Resource, error) { return nil, nil, nil }
	d.Conftest = func(_ []string, _ []byte) ([]byte, error) {
		return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
	}
	var out, errb bytes.Buffer
	if code := Run(d, &out, &errb); code != exitError {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if out.Len() != 0 || !strings.Contains(errb.String(), "resolve previous desired-state revision") {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errb.String())
	}
}

// A host wired without transition support must fail closed when a transition
// bundle matches, never silently skip the phase.
func TestRunTransitionUnsupportedHostFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mutit func(*Deps)
	}{
		{"no transition deps at all", func(d *Deps) { d.Transition = nil }},
		{"resolver missing", func(d *Deps) {
			d.Transition = &TransitionDeps{
				RenderRevision: func(string, string, string) ([]byte, []render.Resource, error) { return nil, nil, nil },
			}
		}},
		{"renderer missing", func(d *Deps) {
			d.Transition = &TransitionDeps{
				LastSuccessfulSource: func(string) (SyncedSource, bool, error) { return SyncedSource{}, false, nil },
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := transitionDeps(t, "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 1}\n")
			tc.mutit(&d)
			d.Conftest = func(_ []string, _ []byte) ([]byte, error) {
				return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
			}
			var out, errb bytes.Buffer
			if code := Run(d, &out, &errb); code != exitError {
				t.Fatalf("code=%d stderr=%s", code, errb.String())
			}
			if out.Len() != 0 {
				t.Fatal("must NOT emit manifests when a transition bundle cannot be evaluated")
			}
			if !strings.Contains(errb.String(), "cannot evaluate transitions") {
				t.Fatalf("stderr=%q", errb.String())
			}
		})
	}
}

// The Application identity is what anchors the transition to a baseline;
// without it there is nothing trustworthy to enforce against.
func TestRunTransitionMissingAppIdentityFailsClosed(t *testing.T) {
	for _, missing := range []string{"ARGOCD_APP_NAME", "ARGOCD_APP_REVISION"} {
		t.Run(missing, func(t *testing.T) {
			current := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 1}\n"
			d := transitionDeps(t, current)
			inner := d.Getenv
			d.Getenv = func(k string) string {
				if k == missing {
					return ""
				}
				return inner(k)
			}
			d.Transition.LastSuccessfulSource = func(string) (SyncedSource, bool, error) {
				t.Fatal("must not reach the Argo CD API without a validated Application identity")
				return SyncedSource{}, false, nil
			}
			d.Transition.RenderRevision = func(string, string, string) ([]byte, []render.Resource, error) {
				return nil, nil, nil
			}
			d.Conftest = func(_ []string, _ []byte) ([]byte, error) {
				return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
			}
			var out, errb bytes.Buffer
			if code := Run(d, &out, &errb); code != exitError {
				t.Fatalf("code=%d stderr=%s", code, errb.String())
			}
			if out.Len() != 0 {
				t.Fatal("must NOT emit manifests without a validated Application identity")
			}
			if !strings.Contains(errb.String(), missing) {
				t.Fatalf("error should name the missing variable, got %q", errb.String())
			}
		})
	}
}
