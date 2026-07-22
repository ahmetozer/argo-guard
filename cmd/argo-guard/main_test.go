package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRunCleansWorkDirOnEarlyConfigurationError(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)
	t.Setenv("GUARD_POLICY_FLAT", "invalid")

	if got := run([]string{"argo-guard", "generate"}); got != 2 {
		t.Fatalf("run()=%d, want configuration error exit code 2", got)
	}

	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		t.Fatalf("read temp root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary directories leaked after early exit: %v", entries)
	}
}

func TestWithGitAuthDisabledWithoutPassword(t *testing.T) {
	args := []string{"clone", "--depth", "1", "https://github.com/org/repo.git", "/dst"}
	got := withGitAuth(args, false)
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("args must be unchanged without a password, got %v", got)
	}
}

func TestWithGitAuthPrependsCredentialHelper(t *testing.T) {
	got := withGitAuth([]string{"clone", "https://github.com/org/repo.git", "/dst"}, true)
	if got[0] != "-c" || got[1] != "credential.helper=" {
		t.Fatalf("must first clear existing credential helpers, got %v", got[:2])
	}
	if got[2] != "-c" || !strings.HasPrefix(got[3], "credential.helper=!") {
		t.Fatalf("must add an inline credential helper, got %v", got[2:4])
	}
	// The helper must defer to the environment at git runtime so the secret
	// never appears in the command line.
	if !strings.Contains(got[3], "${GUARD_POLICY_PASSWORD}") {
		t.Fatalf("helper must read GUARD_POLICY_PASSWORD from env, got %q", got[3])
	}
	if !strings.Contains(got[3], "${GUARD_POLICY_USERNAME:-x-access-token}") {
		t.Fatalf("helper must default the username, got %q", got[3])
	}
	if got[len(got)-1] != "/dst" || got[len(got)-2] != "https://github.com/org/repo.git" {
		t.Fatalf("original args must follow the config flags, got %v", got)
	}
}
