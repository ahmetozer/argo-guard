package trust

import (
	"reflect"
	"strings"
	"testing"
)

func TestFromEnv(t *testing.T) {
	t.Setenv("ARGOCD_ENV_TIER", "frontend")
	t.Setenv("ARGOCD_ENV_REGION", "eu")

	env := map[string]string{
		"ARGOCD_APP_SOURCE_REPO_URL": "https://git.corp/infra/platform.git",
		"ARGOCD_APP_PROJECT_NAME":    "platform",
		"ARGOCD_APP_NAMESPACE":       "ingress",
		"UNRELATED":                  "x",
	}
	got := FromEnv(func(k string) string { return env[k] })

	want := Context{
		Repo:      "https://git.corp/infra/platform.git",
		Project:   "platform",
		Namespace: "ingress",
		AppLabels: map[string]string{"tier": "frontend", "region": "eu"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestFromEnvEmpty(t *testing.T) {
	got := FromEnv(func(string) string { return "" })
	if got.Repo != "" || len(got.AppLabels) != 0 {
		t.Fatalf("expected empty context, got %+v", got)
	}
}

func TestAppRefFromEnv(t *testing.T) {
	env := map[string]string{
		"ARGOCD_APP_NAME":        "payments",
		"ARGOCD_APP_REVISION":    "9f2c1ab",
		"ARGOCD_APP_SOURCE_PATH": "deploy/prod",
		"UNRELATED":              "x",
	}
	got := AppRefFromEnv(func(k string) string { return env[k] })
	want := AppRef{Name: "payments", Revision: "9f2c1ab", SourcePath: "deploy/prod"}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("complete AppRef must validate: %v", err)
	}
}

func TestAppRefValidate(t *testing.T) {
	// An empty source path is the repo root, not a missing value.
	rootPath := AppRef{Name: "payments", Revision: "9f2c1ab"}
	if err := rootPath.Validate(); err != nil {
		t.Fatalf("empty SourcePath means repo root and must validate: %v", err)
	}

	for _, tc := range []struct {
		name string
		ref  AppRef
		want string
	}{
		{"no name", AppRef{Revision: "9f2c1ab"}, "ARGOCD_APP_NAME"},
		{"no revision", AppRef{Name: "payments"}, "ARGOCD_APP_REVISION"},
		{"neither", AppRef{}, "ARGOCD_APP_NAME and ARGOCD_APP_REVISION"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.ref.Validate()
			if err == nil {
				t.Fatal("incomplete AppRef must not validate")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should name %q", err, tc.want)
			}
		})
	}
}
