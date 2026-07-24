//go:build integration

package apprepo

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRenderRevisionFetchesPrivateRepositoryWithAskPassWithoutLeakingSecrets(t *testing.T) {
	for _, binary := range []string{"git", "kustomize"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s is required", binary)
		}
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	runGitCommand(t, "", "init", "--quiet", source)
	runGitCommand(t, source, "config", "user.email", "argo-guard@example.invalid")
	runGitCommand(t, source, "config", "user.name", "Argo Guard Test")
	writeTestFile(t, filepath.Join(source, "kustomization.yaml"), "resources:\n  - configmap.yaml\n", 0o600)
	writeTestFile(t, filepath.Join(source, "configmap.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: private-revision\n", 0o600)
	runGitCommand(t, source, "add", ".")
	runGitCommand(t, source, "commit", "--quiet", "-m", "private revision")
	revision := strings.TrimSpace(runGitOutput(t, source, "rev-parse", "HEAD"))

	bare := filepath.Join(root, "repo.git")
	runGitCommand(t, "", "clone", "--quiet", "--bare", source, bare)

	username, password, nonce := "private-user", "private-password", "private-nonce"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != username || pass != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="private-git"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		serveGitHTTP(t, root, w, r)
	}))
	defer server.Close()

	askPass := filepath.Join(root, "askpass")
	writeTestFile(t, askPass, `#!/bin/sh
case "$1" in
  *Username*) printf '%s\n' "$TEST_GIT_USERNAME" ;;
  *Password*) printf '%s\n' "$TEST_GIT_PASSWORD" ;;
  *) exit 1 ;;
esac
`, 0o700)
	var gitStderr bytes.Buffer
	git := func(workdir string, args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = workdir
		cmd.Env = append(os.Environ(),
			"GIT_ASKPASS="+askPass,
			"GIT_TERMINAL_PROMPT=0",
			"TEST_GIT_USERNAME="+username,
			"TEST_GIT_PASSWORD="+password,
			"ARGOCD_GIT_ASKPASS_NONCE="+nonce,
			"ARGOCD_ASK_PASS_SOCK=/var/run/argocd-askpass/reposerver.sock",
		)
		cmd.Stderr = &gitStderr
		return cmd.Run()
	}
	kustomize := func(dir string) ([]byte, error) {
		cmd := exec.Command("kustomize", "build", dir)
		return cmd.CombinedOutput()
	}
	raw, resources, err := RenderRevision(server.URL+"/repo.git", revision, ".", root, git, kustomize)
	if err != nil {
		t.Fatalf("render private revision: %v\ngit stderr: %s", err, gitStderr.String())
	}
	if len(resources) != 1 || resources[0].Name != "private-revision" {
		t.Fatalf("resources=%+v", resources)
	}
	combined := string(raw) + gitStderr.String()
	for _, secret := range []string{username, password, nonce} {
		if strings.Contains(combined, secret) {
			t.Fatalf("credential or nonce leaked in output: %q", secret)
		}
	}
}

func serveGitHTTP(t *testing.T, projectRoot string, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	cmd := exec.Command("git", "http-backend")
	cmd.Env = append(os.Environ(),
		"GIT_PROJECT_ROOT="+projectRoot,
		"GIT_HTTP_EXPORT_ALL=1",
		"PATH_INFO="+r.URL.Path,
		"QUERY_STRING="+r.URL.RawQuery,
		"REQUEST_METHOD="+r.Method,
		"CONTENT_TYPE="+r.Header.Get("Content-Type"),
		"CONTENT_LENGTH="+strconv.FormatInt(r.ContentLength, 10),
		"SERVER_PROTOCOL="+r.Proto,
	)
	cmd.Stdin = r.Body
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("git http-backend: %v: %s", err, stderr.String())
		http.Error(w, "git backend failed", http.StatusInternalServerError)
		return
	}

	reader := bufio.NewReader(&stdout)
	status := http.StatusOK
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Errorf("read git backend headers: %v", err)
			http.Error(w, "invalid git backend response", http.StatusInternalServerError)
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			t.Errorf("invalid git backend header %q", line)
			http.Error(w, "invalid git backend response", http.StatusInternalServerError)
			return
		}
		value = strings.TrimSpace(value)
		if strings.EqualFold(name, "Status") {
			code, err := strconv.Atoi(strings.Fields(value)[0])
			if err != nil {
				t.Errorf("invalid git backend status %q", value)
				http.Error(w, "invalid git backend response", http.StatusInternalServerError)
				return
			}
			status = code
			continue
		}
		w.Header().Add(name, value)
	}
	w.WriteHeader(status)
	if _, err := io.Copy(w, reader); err != nil {
		t.Errorf("write git backend response: %v", err)
	}
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	if output, err := runCommand(dir, "git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := runCommand(dir, "git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func runCommand(dir, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd
}

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(fmt.Errorf("write %s: %w", filepath.Base(path), err))
	}
}
