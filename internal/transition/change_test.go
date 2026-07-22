package transition

import (
	"strings"
	"testing"

	"github.com/ahmetozer/argo-guard/internal/render"
)

func resources(t *testing.T, yaml string) []render.Resource {
	t.Helper()
	_, resources, err := render.Build(".", func(string) ([]byte, error) { return []byte(yaml), nil })
	if err != nil {
		t.Fatal(err)
	}
	return resources
}

func TestDiffBuildsUpdateCreateAndDelete(t *testing.T) {
	before := resources(t, `apiVersion: apps/v1
kind: Deployment
metadata: {name: web, namespace: prod}
spec: {replicas: 2}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: old, namespace: prod}
`)
	after := resources(t, `apiVersion: apps/v1
kind: Deployment
metadata: {name: web, namespace: prod}
spec: {replicas: 0}
---
apiVersion: v1
kind: Service
metadata: {name: new, namespace: prod}
`)
	changes, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("changes=%+v", changes)
	}
	operations := map[string]string{}
	for _, change := range changes {
		operations[change.Spec.Resource.Name] = change.Spec.Operation
	}
	if operations["web"] != "Update" || operations["old"] != "Delete" || operations["new"] != "Create" {
		t.Fatalf("operations=%v", operations)
	}
}

func TestDiffSkipsUnchangedResources(t *testing.T) {
	manifest := resources(t, "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 0}\n")
	changes, err := Diff(manifest, manifest)
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
}

func TestSummariesDoNotDuplicateManifestBodies(t *testing.T) {
	before := resources(t, "apiVersion: v1\nkind: ServiceAccount\nmetadata: {name: app, namespace: prod}\n")
	changes, err := Diff(before, nil)
	if err != nil {
		t.Fatal(err)
	}
	summaries := Summaries(changes)
	if len(summaries) != 1 || summaries[0].Operation != "Delete" || summaries[0].Resource.Name != "app" {
		t.Fatalf("summaries=%+v", summaries)
	}
}

func TestDiffRejectsDuplicateIdentity(t *testing.T) {
	duplicate := resources(t, `apiVersion: v1
kind: ConfigMap
metadata: {name: x}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: x}
`)
	if _, err := Diff(nil, duplicate); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err=%v", err)
	}
}

func TestEncodeManifestChange(t *testing.T) {
	after := resources(t, "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\nspec: {replicas: 0}\n")
	changes, err := Diff(nil, after)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Encode(changes)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"kind: ManifestChange", "operation: Create", "replicas: 0"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("encoded change missing %q:\n%s", want, raw)
		}
	}
}
