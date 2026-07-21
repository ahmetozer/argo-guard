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
	out, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// ConfigMap delivery mode: flat expansion on, policies ConfigMap mounted.
	for _, want := range []string{"argo-guard", "GUARD_POLICY_FLAT", "argo-guard-policies"} {
		if !contains(s, want) {
			t.Fatalf("patch missing %q:\n%s", want, s)
		}
	}
	// No git in the sidecar: the policy repo env must be gone entirely.
	if contains(s, "GUARD_POLICY_REPO") {
		t.Fatalf("patch must not configure GUARD_POLICY_REPO in ConfigMap mode:\n%s", s)
	}
	// The policy mount must not use subPath — it would freeze kubelet updates.
	if contains(s, "subPath: guard") || countSubPath(s) > 1 {
		t.Fatalf("policy mount must not use subPath (only plugin.yaml may):\n%s", s)
	}
}

func countSubPath(s string) int {
	n, i := 0, 0
	for {
		j := indexOf(s[i:], "subPath")
		if j < 0 {
			return n
		}
		n++
		i += j + len("subPath")
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
