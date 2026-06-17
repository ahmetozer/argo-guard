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
	res, err := Run([]byte("kind: Service\n"), ctx, "/policies", []string{"global", "projects/infra"}, work, run)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Denies) != 1 || res.Denies[0].Msg != "LoadBalancer not allowed" {
		t.Fatalf("denies=%+v", res.Denies)
	}
	if len(res.Warns) != 1 {
		t.Fatalf("warns=%+v", res.Warns)
	}

	// context.json must be written and passed via --data.
	ctxPath := filepath.Join(work, "context.json")
	raw, err := os.ReadFile(ctxPath)
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
	if !containsArg(gotArgs, "--data") || !containsArg(gotArgs, work) {
		t.Fatalf("expected --data %s in args: %v", work, gotArgs)
	}
	if !containsArg(gotArgs, filepath.Join("/policies", "global")) {
		t.Fatalf("expected policy dir in args: %v", gotArgs)
	}
	if string(gotStdin) != "kind: Service\n" {
		t.Fatalf("stdin should be rendered manifests, got %q", gotStdin)
	}
}

func TestRunExecErrorFailsClosed(t *testing.T) {
	run := func([]string, []byte) ([]byte, error) { return nil, errors.New("conftest crashed") }
	_, err := Run([]byte("x"), trust.Context{}, "/p", []string{"global"}, t.TempDir(), run)
	if err == nil {
		t.Fatal("conftest execution error must fail closed")
	}
}

func TestRunCleanPasses(t *testing.T) {
	run := func([]string, []byte) ([]byte, error) {
		return []byte(`[{"filename":"-","failures":[],"warnings":[]}]`), nil
	}
	res, err := Run([]byte("x"), trust.Context{}, "/p", []string{"global"}, t.TempDir(), run)
	if err != nil || len(res.Denies) != 0 || len(res.Warns) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want || strings.Contains(a, want) {
			return true
		}
	}
	return false
}
