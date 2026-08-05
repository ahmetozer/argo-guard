package evaluate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmetozer/argo-guard/internal/trust"
)

func TestRunParsesFailures(t *testing.T) {
	out := `[{"filename":"-","failures":[{"msg":"LoadBalancer not allowed"}],"warnings":[{"msg":"missing label"}]}]`
	var gotArgs []string
	var gotStdin []byte
	run := func(args []string, stdin []byte) ([]byte, error) {
		gotArgs = args
		gotStdin = stdin
		return []byte(out), nil
	}
	work := t.TempDir()
	ctx := trust.Context{Repo: "https://git/infra.git", Project: "platform"}
	res, err := Run([]byte("kind: Service\n"), ctx, "/policies", []string{"global", "projects/infra"}, work, nil, run)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Denies) != 1 || res.Denies[0].Msg != "LoadBalancer not allowed" {
		t.Fatalf("denies=%+v", res.Denies)
	}
	if len(res.Warns) != 1 {
		t.Fatalf("warns=%+v", res.Warns)
	}

	// context.json must be written into the dir handed to conftest via --data.
	dataDir := firstDataDir(t, gotArgs)
	raw, err := os.ReadFile(filepath.Join(dataDir, "context.json"))
	if err != nil {
		t.Fatalf("context.json not written: %v", err)
	}
	var wrapped struct {
		Context trust.Context `json:"context"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		t.Fatal(err)
	}
	if wrapped.Context.Project != "platform" {
		t.Fatalf("context.json wrong: %s", raw)
	}
	if !containsArg(gotArgs, filepath.Join("/policies", "global")) {
		t.Fatalf("expected policy dir in args: %v", gotArgs)
	}
	// Each bundle dir must be passed as --data so data.json files (e.g.
	// trustedRepos) are loaded by conftest. Regression lock for C1.
	for _, bundleDir := range []string{"global", "projects/infra"} {
		want := filepath.Join("/policies", bundleDir)
		var foundPolicy, foundData bool
		for i, a := range gotArgs {
			if a == "--policy" && i+1 < len(gotArgs) && gotArgs[i+1] == want {
				foundPolicy = true
			}
			if a == "--data" && i+1 < len(gotArgs) && gotArgs[i+1] == want {
				foundData = true
			}
		}
		if !foundPolicy {
			t.Fatalf("bundle %q missing --policy flag: %v", bundleDir, gotArgs)
		}
		if !foundData {
			t.Fatalf("bundle %q missing --data flag (data.json would not be loaded): %v", bundleDir, gotArgs)
		}
	}
	if string(gotStdin) != "kind: Service\n" {
		t.Fatalf("stdin should be rendered manifests, got %q", gotStdin)
	}
}

func TestRunExecErrorFailsClosed(t *testing.T) {
	run := func([]string, []byte) ([]byte, error) { return nil, errors.New("conftest crashed") }
	_, err := Run([]byte("x"), trust.Context{}, "/p", []string{"global"}, t.TempDir(), nil, run)
	if err == nil {
		t.Fatal("conftest execution error must fail closed")
	}
}

func TestRunCleanPasses(t *testing.T) {
	run := func([]string, []byte) ([]byte, error) {
		return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
	}
	res, err := Run([]byte("x"), trust.Context{}, "/p", []string{"global"}, t.TempDir(), nil, run)
	if err != nil || len(res.Denies) != 0 || len(res.Warns) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRunWritesRuntimeData(t *testing.T) {
	work := t.TempDir()
	var dataDir string
	run := func(args []string, _ []byte) ([]byte, error) {
		dataDir = firstDataDir(t, args)
		raw, err := os.ReadFile(filepath.Join(dataDir, "runtime-data.json"))
		if err != nil {
			t.Fatalf("runtime data not written: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got["transition"] == nil {
			t.Fatalf("transition runtime data missing: %s", raw)
		}
		return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
	}

	_, err := Run(
		[]byte("x"), trust.Context{}, "/p", []string{"transitions"}, work,
		map[string]any{"transition": map[string]any{"afterResources": []any{}}}, run,
	)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dataDir, "runtime-data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("runtime data mode=%o, want 600", info.Mode().Perm())
	}
}

func TestRunRejectsRuntimeDataContextOverride(t *testing.T) {
	_, err := Run(
		[]byte("x"), trust.Context{}, "/p", []string{"transitions"}, t.TempDir(),
		map[string]any{"context": "spoofed"},
		func([]string, []byte) ([]byte, error) { return []byte(`[]`), nil },
	)
	if err == nil || !strings.Contains(err.Error(), "reserved data.context") {
		t.Fatalf("expected reserved context error, got %v", err)
	}
}

// Regression lock: conftest must receive a purpose-built data directory, never
// the scratch root. Callers legitimately keep other state under workdir — a
// previous-revision git checkout, most importantly — and none of it may be
// reachable as OPA data.
func TestRunIsolatesPolicyDataFromScratchRoot(t *testing.T) {
	work := t.TempDir()
	checkout := filepath.Join(work, "app-revision-abc123")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "kustomization.yaml"), []byte("resources: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var gotArgs []string
	run := func(args []string, _ []byte) ([]byte, error) {
		gotArgs = args
		return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
	}
	if _, err := Run([]byte("x"), trust.Context{}, "/p", []string{"global"}, work, nil, run); err != nil {
		t.Fatal(err)
	}

	dataDir := firstDataDir(t, gotArgs)
	if dataDir == work {
		t.Fatal("conftest received the scratch root as --data; unrelated scratch state would load as policy data")
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "context.json" && e.Name() != "runtime-data.json" {
			t.Fatalf("unexpected entry in policy data dir: %s", e.Name())
		}
	}
}

// Each evaluation phase gets its own data dir, so a phase without runtime data
// cannot observe an earlier phase's. This invariant replaces the stale-file
// deletion that a shared data dir used to require.
func TestRunGivesEachPhaseAnIsolatedDataDir(t *testing.T) {
	work := t.TempDir()
	var dirs []string
	run := func(args []string, _ []byte) ([]byte, error) {
		dirs = append(dirs, firstDataDir(t, args))
		return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
	}

	withData := map[string]any{"transition": map[string]any{"afterResources": []any{}}}
	if _, err := Run([]byte("x"), trust.Context{}, "/p", []string{"transitions"}, work, withData, run); err != nil {
		t.Fatal(err)
	}
	if _, err := Run([]byte("x"), trust.Context{}, "/p", []string{"global"}, work, nil, run); err != nil {
		t.Fatal(err)
	}

	if dirs[0] == dirs[1] {
		t.Fatalf("phases shared a data dir: %s", dirs[0])
	}
	if _, err := os.Stat(filepath.Join(dirs[1], "runtime-data.json")); !os.IsNotExist(err) {
		t.Fatal("phase with no runtime data must not inherit the previous phase's runtime-data.json")
	}
}

// firstDataDir returns the leading --data value: the isolated directory built
// for this evaluation, before any bundle data dirs.
func firstDataDir(t *testing.T, args []string) string {
	t.Helper()
	for i, a := range args {
		if a == "--data" && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("no --data flag in args: %v", args)
	return ""
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want || strings.Contains(a, want) {
			return true
		}
	}
	return false
}
