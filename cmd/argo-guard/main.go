// Command argo-guard is an Argo CD Config Management Plugin that validates
// rendered Kustomize manifests against layered Conftest/Rego policies.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/ahmetozer/argo-guard/internal/generate"
	"github.com/ahmetozer/argo-guard/internal/policyrepo"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "generate" {
		fmt.Fprintln(os.Stderr, "usage: argo-guard generate")
		os.Exit(2)
	}

	cacheDir := getenvDefault("GUARD_POLICY_CACHE", "/var/cache/argo-guard/policies")
	ttl := parseTTL(getenvDefault("GUARD_POLICY_TTL", "60s"))
	workDir, err := os.MkdirTemp("", "argo-guard-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "argo-guard: workdir: %v\n", err)
		os.Exit(2)
	}
	defer os.RemoveAll(workDir)

	cache := policyrepo.New(
		os.Getenv("GUARD_POLICY_REPO"),
		getenvDefault("GUARD_POLICY_REF", "main"),
		cacheDir, ttl,
		func(workdir string, args ...string) error {
			cmd := exec.Command("git", args...)
			if workdir != "" {
				cmd.Dir = workdir
			}
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		time.Now,
	)

	deps := generate.Deps{
		Getenv: os.Getenv,
		Kustomize: func(path string) ([]byte, error) {
			cmd := exec.Command("kustomize", "build", path)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return nil, err
			}
			return out.Bytes(), nil
		},
		Conftest: conftestRunner,
		EnsurePolicies: func() (string, bool, error) {
			return cache.Ensure()
		},
		WorkDir: workDir,
	}

	os.Exit(generate.Run(deps, os.Stdout, os.Stderr))
}

// conftestRunner runs conftest. conftest exits non-zero on policy failure but
// still prints JSON, so a non-zero exit with output is NOT an execution error.
func conftestRunner(args []string, stdin []byte) ([]byte, error) {
	cmd := exec.Command("conftest", args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok && out.Len() > 0 {
			return out.Bytes(), nil // policy failures, JSON present
		}
		return nil, fmt.Errorf("%w: %s", err, errb.String())
	}
	return out.Bytes(), nil
}

func getenvDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func parseTTL(s string) time.Duration {
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second
	}
	return time.Minute
}
