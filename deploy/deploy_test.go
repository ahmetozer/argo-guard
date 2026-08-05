package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	// Least privilege by default: the shipped plugin must not hand application
	// repo credentials to the sidecar. Operators opt in per install when they
	// add a `mode: transition` bundle.
	if _, present := spec["provideGitCreds"]; present {
		t.Fatal("provideGitCreds must stay commented out; transition mode is opt-in")
	}
}

func TestApplicationReaderRBACIsReadOnly(t *testing.T) {
	m := parseYAML(t, "application-reader-rbac.yaml")
	items, ok := m["items"].([]any)
	if !ok {
		t.Fatal("RBAC manifest must contain items")
	}
	var role map[string]any
	for _, item := range items {
		object, _ := item.(map[string]any)
		if object["kind"] == "Role" {
			role = object
		}
	}
	if role == nil || nestedMap(t, role, "metadata")["namespace"] != "argocd" {
		t.Fatal("application reader must be a Role in the argocd namespace")
	}
	rules, ok := role["rules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("Role rules=%v", role["rules"])
	}
	rule, _ := rules[0].(map[string]any)
	if !reflect.DeepEqual(rule["apiGroups"], []any{"argoproj.io"}) ||
		!reflect.DeepEqual(rule["resources"], []any{"applications"}) ||
		!reflect.DeepEqual(rule["verbs"], []any{"get"}) {
		t.Fatalf("Role must grant only get on applications.argoproj.io: %v", rule)
	}
	out, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"applications", "get", "argocd-repo-server"} {
		if !contains(s, want) {
			t.Fatalf("RBAC missing %q:\n%s", want, s)
		}
	}
	for _, forbidden := range []string{"create", "update", "patch", "delete", "list", "watch"} {
		if contains(s, "- "+forbidden+"\n") {
			t.Fatalf("RBAC must not grant %q:\n%s", forbidden, s)
		}
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

func TestRepoServerPatchRendersIsolatedCredentialsAndAskPass(t *testing.T) {
	if _, err := exec.LookPath("kustomize"); err != nil {
		t.Skip("kustomize is required for rendered deployment test")
	}
	dir := t.TempDir()
	base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: argocd-repo-server
  namespace: argocd
spec:
  selector:
    matchLabels: {app.kubernetes.io/name: argocd-repo-server}
  template:
    metadata:
      labels: {app.kubernetes.io/name: argocd-repo-server}
    spec:
      serviceAccountName: argocd-repo-server
      automountServiceAccountToken: false
      containers:
        - name: argocd-repo-server
          image: quay.io/argoproj/argocd:v3.4.5
          volumeMounts:
            - {name: tmp, mountPath: /tmp}
            - {name: plugins, mountPath: /home/argocd/cmp-server/plugins}
      volumes:
        - {name: tmp, emptyDir: {}}
        - {name: var-files, emptyDir: {}}
        - {name: plugins, emptyDir: {}}
`
	patch, err := os.ReadFile("repo-server-patch.yaml")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"base.yaml":  []byte(base),
		"patch.yaml": patch,
		"kustomization.yaml": []byte(`resources:
  - base.yaml
patches:
  - path: patch.yaml
`),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("kustomize", "build", dir)
	rendered, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kustomize build: %v\n%s", err, rendered)
	}
	var deployment map[string]any
	if err := yaml.Unmarshal(rendered, &deployment); err != nil {
		t.Fatal(err)
	}
	podSpec := nestedMap(t, deployment, "spec", "template", "spec")
	if podSpec["automountServiceAccountToken"] != false {
		t.Fatalf("automountServiceAccountToken=%v", podSpec["automountServiceAccountToken"])
	}
	containers := namedObjects(t, podSpec["containers"])
	repoServer := containers["argocd-repo-server"]
	guard := containers["argo-guard"]
	assertEnv(t, repoServer, "ARGOCD_ASK_PASS_SOCK", "/var/run/argocd-askpass/reposerver.sock")
	assertEnv(t, guard, "ARGOCD_ASK_PASS_SOCK", "/var/run/argocd-askpass/reposerver.sock")
	assertMount(t, repoServer, "argocd-askpass", "/var/run/argocd-askpass", false)
	assertMount(t, guard, "argocd-askpass", "/var/run/argocd-askpass", false)
	assertMount(t, guard, "argo-guard-kube-api", "/var/run/secrets/kubernetes.io/serviceaccount", true)
	if hasMount(repoServer, "argo-guard-kube-api") {
		t.Fatal("repo-server container must not receive the Kubernetes API token")
	}
	assertMount(t, repoServer, "tmp", "/tmp", false)
	assertMount(t, guard, "cmp-tmp", "/tmp", false)
	volumes := namedObjects(t, podSpec["volumes"])
	if volumes["argocd-askpass"]["emptyDir"] == nil {
		t.Fatal("argocd-askpass must be an emptyDir")
	}
	projected, ok := volumes["argo-guard-kube-api"]["projected"].(map[string]any)
	if !ok {
		t.Fatal("argo-guard-kube-api must be projected")
	}
	raw, _ := yaml.Marshal(projected)
	for _, want := range []string{"serviceAccountToken", "expirationSeconds: 3600", "kube-root-ca.crt", "path: ca.crt"} {
		if !contains(string(raw), want) {
			t.Fatalf("projected volume missing %q:\n%s", want, raw)
		}
	}
}

func nestedMap(t *testing.T, root map[string]any, keys ...string) map[string]any {
	t.Helper()
	current := root
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("%s is not an object", key)
		}
		current = next
	}
	return current
}

func namedObjects(t *testing.T, raw any) map[string]map[string]any {
	t.Helper()
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected list, got %T", raw)
	}
	out := make(map[string]map[string]any, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected object, got %T", item)
		}
		name, _ := object["name"].(string)
		out[name] = object
	}
	return out
}

func assertEnv(t *testing.T, container map[string]any, name, value string) {
	t.Helper()
	for _, env := range namedObjects(t, container["env"]) {
		if env["name"] == name && env["value"] == value {
			return
		}
	}
	t.Fatalf("container %v missing env %s=%s", container["name"], name, value)
}

func assertMount(t *testing.T, container map[string]any, name, path string, readOnly bool) {
	t.Helper()
	mounts := namedObjects(t, container["volumeMounts"])
	mount, ok := mounts[name]
	if !ok || mount["mountPath"] != path {
		t.Fatalf("container %v missing mount %s at %s", container["name"], name, path)
	}
	if readOnly && mount["readOnly"] != true {
		t.Fatalf("mount %s must be read-only", name)
	}
}

func hasMount(container map[string]any, name string) bool {
	items, _ := container["volumeMounts"].([]any)
	for _, item := range items {
		mount, _ := item.(map[string]any)
		if mount["name"] == name {
			return true
		}
	}
	return false
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
