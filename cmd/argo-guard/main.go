// Command argo-guard is an Argo CD Config Management Plugin that validates
// rendered Kustomize manifests against layered Conftest/Rego policies.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ahmetozer/argo-guard/internal/apprepo"
	"github.com/ahmetozer/argo-guard/internal/argocd"
	"github.com/ahmetozer/argo-guard/internal/generate"
	"github.com/ahmetozer/argo-guard/internal/policyflat"
	"github.com/ahmetozer/argo-guard/internal/policyrepo"
	"github.com/ahmetozer/argo-guard/internal/render"
)

const mountedArgoCDGitAskPass = "/var/run/argocd/argocd"

func main() {
	os.Exit(run(os.Args))
}

func run(args []string) (code int) {
	if len(args) < 2 || args[1] != "generate" {
		fmt.Fprintln(os.Stderr, "usage: argo-guard generate")
		return 2
	}

	cacheDir := getenvDefault("GUARD_POLICY_CACHE", "/var/cache/argo-guard/policies")
	ttl := parseTTL(getenvDefault("GUARD_POLICY_TTL", "60s"))
	workDir, err := os.MkdirTemp("", "argo-guard-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "argo-guard: workdir: %v\n", err)
		return 2
	}
	cleanups := []string{workDir}
	// main calls os.Exit only after run returns, so this defer also runs for
	// configuration failures and every fail-closed generate result.
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			if err := os.RemoveAll(cleanups[i]); err != nil {
				fmt.Fprintf(os.Stderr, "argo-guard: remove temporary directory %s: %v\n", cleanups[i], err)
				code = 2
			}
		}
	}()

	cache := policyrepo.New(
		os.Getenv("GUARD_POLICY_REPO"),
		getenvDefault("GUARD_POLICY_REF", "main"),
		cacheDir, ttl,
		func(workdir string, args ...string) error {
			cmd := exec.Command("git", withGitAuth(args, os.Getenv("GUARD_POLICY_PASSWORD") != "")...)
			if workdir != "" {
				cmd.Dir = workdir
			}
			// Never fall back to an interactive prompt; fail with a clear error.
			cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		time.Now,
	)

	flat, err := policyflat.Mode(os.Getenv("GUARD_POLICY_FLAT"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "argo-guard: %v\n", err)
		return 2
	}
	if flat && os.Getenv("GUARD_POLICY_REPO") != "" {
		fmt.Fprintln(os.Stderr, "argo-guard: GUARD_POLICY_FLAT requires GUARD_POLICY_REPO to be unset")
		return 2
	}

	ensure := func() (string, bool, error) { return cache.Ensure() }
	if os.Getenv("GUARD_POLICY_REPO") == "" {
		// No-git mode: use the pre-seeded cache dir as-is — either a plain
		// policy tree (local/dev) or a flattened ConfigMap mount (GUARD_POLICY_FLAT).
		dir := cacheDir
		ensure = func() (string, bool, error) {
			if _, err := os.Stat(dir); err != nil {
				return "", false, fmt.Errorf("GUARD_POLICY_REPO unset and no cache at %s (fail-closed)", dir)
			}
			if !flat {
				return dir, false, nil
			}
			// Expand outside workDir: conftest recurses --data <workDir>, and an
			// expanded tree there would double-load the bundle data files.
			dst, err := os.MkdirTemp("", "argo-guard-policies-")
			if err != nil {
				return "", false, fmt.Errorf("flat policy expansion: %w", err)
			}
			cleanups = append(cleanups, dst)
			if err := policyflat.Expand(dir, dst); err != nil {
				return "", false, err
			}
			return dst, false, nil
		}
	}

	runKustomize := func(path string) ([]byte, error) {
		cmd := exec.Command("kustomize", "build", path)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
	runAppGit := func(workdir string, args ...string) error {
		cmd := exec.Command("git", args...)
		if workdir != "" {
			cmd.Dir = workdir
		}
		cmd.Env = applicationGitEnv(os.Environ())
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	deps := generate.Deps{
		Getenv:         os.Getenv,
		Kustomize:      runKustomize,
		Conftest:       conftestRunner,
		EnsurePolicies: ensure,
		Transition: &generate.TransitionDeps{
			LastSuccessfulSource: func(appName string) (generate.SyncedSource, bool, error) {
				client, err := argocd.NewInCluster(getenvDefault("GUARD_ARGOCD_NAMESPACE", "argocd"))
				if err != nil {
					return generate.SyncedSource{}, false, err
				}
				source, found, err := client.LastSuccessfulSource(appName)
				return generate.SyncedSource{
					RepoURL:  source.RepoURL,
					Revision: source.Revision,
					Path:     source.Path,
					Destination: generate.Destination{
						Server:    source.Destination.Server,
						Name:      source.Destination.Name,
						Namespace: source.Destination.Namespace,
					},
				}, found, err
			},
			// The checkout lands under workDir alongside the per-phase policy
			// data dirs, but conftest only ever receives the latter, so a
			// checked-out repo cannot leak into OPA's data document.
			RenderRevision: func(repoURL, revision, sourcePath string) ([]byte, []render.Resource, error) {
				return apprepo.RenderRevision(repoURL, revision, sourcePath, workDir, runAppGit, runKustomize)
			},
		},
		WorkDir: workDir,
	}

	return generate.Run(deps, os.Stdout, os.Stderr)
}

// applicationGitEnv preserves Argo CD's short-lived AskPass nonce while
// resolving its multi-call binary to the absolute path mounted by the stock
// repo-server init container. The sidecar image intentionally does not place
// that binary on PATH.
func applicationGitEnv(environ []string) []string {
	out := make([]string, 0, len(environ)+1)
	haveTerminalPrompt := false
	for _, item := range environ {
		switch {
		case item == "GIT_ASKPASS=argocd":
			out = append(out, "GIT_ASKPASS="+mountedArgoCDGitAskPass)
		case strings.HasPrefix(item, "GIT_TERMINAL_PROMPT="):
			out = append(out, "GIT_TERMINAL_PROMPT=0")
			haveTerminalPrompt = true
		default:
			out = append(out, item)
		}
	}
	if !haveTerminalPrompt {
		out = append(out, "GIT_TERMINAL_PROMPT=0")
	}
	return out
}

// withGitAuth prepends an inline git credential helper when a policy-repo
// password is configured (GUARD_POLICY_PASSWORD, optionally with
// GUARD_POLICY_USERNAME). The helper expands the variables at git runtime, so
// the secret never appears in the command line, the repo URL, or git's stderr.
func withGitAuth(args []string, havePassword bool) []string {
	if !havePassword {
		return args
	}
	const helper = `credential.helper=!f() { echo "username=${GUARD_POLICY_USERNAME:-x-access-token}"; echo "password=${GUARD_POLICY_PASSWORD}"; }; f`
	return append([]string{"-c", "credential.helper=", "-c", helper}, args...)
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
