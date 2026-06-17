package generate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
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

func baseDeps(t *testing.T, conftestOut string, conftestErr error) (Deps, string) {
	root := fakePolicyRoot(t)
	d := Deps{
		Getenv:    func(k string) string { return map[string]string{"ARGOCD_APP_SOURCE_PATH": "app/"}[k] },
		Kustomize: func(string) ([]byte, error) { return []byte("kind: Service\nmetadata:\n  name: web\n"), nil },
		Conftest:  func([]string, []byte) ([]byte, error) { return []byte(conftestOut), conftestErr },
		EnsurePolicies: func() (string, bool, error) { return root, false, nil },
		WorkDir:   t.TempDir(),
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
