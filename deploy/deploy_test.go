package deploy

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func parseYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

func TestPluginManifest(t *testing.T) {
	m := parseYAML(t, "plugin.yaml")
	if m["kind"] != "ConfigManagementPlugin" {
		t.Fatalf("kind=%v", m["kind"])
	}
	spec, _ := m["spec"].(map[string]any)
	gen, _ := spec["generate"].(map[string]any)
	if gen["command"] == nil {
		t.Fatal("generate.command required")
	}
	disc, _ := spec["discover"].(map[string]any)
	if disc["find"] == nil {
		t.Fatal("discover.find required")
	}
}

func TestRepoServerPatchHasSidecar(t *testing.T) {
	m := parseYAML(t, "repo-server-patch.yaml")
	// Walk to spec.template.spec.containers and assert argo-guard present.
	out, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(out), "argo-guard") || !contains(string(out), "GUARD_POLICY_REPO") {
		t.Fatalf("patch missing sidecar/env:\n%s", out)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
