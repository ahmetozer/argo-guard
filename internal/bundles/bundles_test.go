package bundles

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ahmetozer/argo-guard/internal/trust"
)

const sample = `
bundles:
  - dir: global
    match: {}
  - dir: projects/infra
    match:
      and:
        - repo: { startsWith: "https://git.corp/infra/" }
    exclude:
      namespace: { equals: [kube-system, argocd] }
  - dir: namespaces/payments
    match:
      namespace: payments
`

func writeRegistry(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "guard.yaml")
	if err := os.WriteFile(p, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSelectGlobalAlways(t *testing.T) {
	r, err := Load(writeRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	got := r.Select(trust.Context{Repo: "https://git.corp/team-a/app.git", Namespace: "default"})
	if !reflect.DeepEqual(got, []string{"global"}) {
		t.Fatalf("got %v", got)
	}
}

func TestSelectInfraWithExclude(t *testing.T) {
	r, _ := Load(writeRegistry(t))
	got := r.Select(trust.Context{Repo: "https://git.corp/infra/p.git", Namespace: "ingress"})
	if !reflect.DeepEqual(got, []string{"global", "projects/infra"}) {
		t.Fatalf("got %v", got)
	}
	excluded := r.Select(trust.Context{Repo: "https://git.corp/infra/p.git", Namespace: "kube-system"})
	if !reflect.DeepEqual(excluded, []string{"global"}) {
		t.Fatalf("exclude should drop infra bundle, got %v", excluded)
	}
}

func TestSelectMultiple(t *testing.T) {
	r, _ := Load(writeRegistry(t))
	got := r.Select(trust.Context{Repo: "https://git.corp/infra/p.git", Namespace: "payments"})
	if !reflect.DeepEqual(got, []string{"global", "projects/infra", "namespaces/payments"}) {
		t.Fatalf("got %v", got)
	}
}
