package apprepo

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRenderRevisionFetchesAndBuildsSourcePath(t *testing.T) {
	var calls [][]string
	git := func(_ string, args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	var buildPath string
	kustomize := func(path string) ([]byte, error) {
		buildPath = path
		return []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n"), nil
	}
	_, resources, err := RenderRevision("https://git.example/app.git", "abc1234", "deploy/prod", t.TempDir(), git, kustomize)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].Name != "web" {
		t.Fatalf("resources=%+v", resources)
	}
	if filepath.Base(buildPath) != "prod" || filepath.Base(filepath.Dir(buildPath)) != "deploy" {
		t.Fatalf("build path=%q", buildPath)
	}
	wantFetch := []string{"fetch", "--depth", "1", "origin", "abc1234"}
	if len(calls) != 4 || !reflect.DeepEqual(calls[2], wantFetch) {
		t.Fatalf("git calls=%v", calls)
	}
}

func TestRenderRevisionRejectsPathEscape(t *testing.T) {
	_, _, err := RenderRevision("https://git.example/app.git", "abc1234", "../prod", t.TempDir(), func(string, ...string) error {
		t.Fatal("git must not run")
		return nil
	}, func(string) ([]byte, error) { return nil, nil })
	if err == nil || !strings.Contains(err.Error(), "inside the repo") {
		t.Fatalf("err=%v", err)
	}
}

func TestRenderRevisionRejectsUnsafeRevision(t *testing.T) {
	_, _, err := RenderRevision("https://git.example/app.git", "--upload-pack=evil", ".", t.TempDir(), func(string, ...string) error { return nil }, func(string) ([]byte, error) { return nil, nil })
	if err == nil || !strings.Contains(err.Error(), "invalid application revision") {
		t.Fatalf("err=%v", err)
	}
}
