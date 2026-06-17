package trust

import (
	"reflect"
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
