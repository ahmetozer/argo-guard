package render

import (
	"errors"
	"testing"
)

const twoDocs = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: payments
---
apiVersion: v1
kind: Service
metadata:
  name: web
`

func TestBuildParsesDocs(t *testing.T) {
	k := func(path string) ([]byte, error) { return []byte(twoDocs), nil }
	raw, res, err := Build("app/", k)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != twoDocs {
		t.Fatal("raw output must be returned verbatim for emit")
	}
	if len(res) != 2 {
		t.Fatalf("want 2 resources, got %d", len(res))
	}
	if res[0].Kind != "Deployment" || res[0].Name != "web" || res[0].Namespace != "payments" {
		t.Fatalf("got %+v", res[0])
	}
	if res[1].Kind != "Service" || res[1].Namespace != "" {
		t.Fatalf("got %+v", res[1])
	}
}

func TestBuildFailsClosedOnKustomizeError(t *testing.T) {
	k := func(string) ([]byte, error) { return nil, errors.New("kustomize boom") }
	if _, _, err := Build("app/", k); err == nil {
		t.Fatal("kustomize error must propagate (fail-closed)")
	}
}

func TestBuildSkipsEmptyDocs(t *testing.T) {
	k := func(string) ([]byte, error) { return []byte("---\n\n---\nkind: ConfigMap\nmetadata:\n  name: c\n"), nil }
	_, res, err := Build("app/", k)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("empty docs should be skipped, got %d", len(res))
	}
}
