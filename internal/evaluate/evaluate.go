// Package evaluate runs conftest against rendered manifests with the trust
// context injected as data.context.
package evaluate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ahmetozer/argo-guard/internal/trust"
)

type Finding struct {
	Rule string
	Msg  string
	File string
}

type Result struct {
	Denies []Finding
	Warns  []Finding
}

// ConftestFunc runs conftest with args, piping stdin, returning stdout. It must
// return a non-nil error only for execution failures, NOT for policy failures
// (conftest exits non-zero on policy failure but still prints JSON; the runner
// should treat a non-zero exit with valid JSON as success).
type ConftestFunc func(args []string, stdin []byte) ([]byte, error)

// conftest JSON output shapes (subset).
type conftestResult struct {
	Filename string `json:"filename"`
	Failures []struct {
		Msg string `json:"msg"`
	} `json:"failures"`
	Warnings []struct {
		Msg string `json:"msg"`
	} `json:"warnings"`
}

// Run evaluates rendered against the selected bundle policy dirs.
//
// runtimeData may be nil. When set it is merged into OPA's data document
// alongside data.context; transition policies use it to inspect the complete
// previous and requested desired states without duplicating them into every
// ManifestChange input.
//
// workdir is a scratch root, NOT the --data directory. Each invocation gets a
// fresh subdirectory holding only context.json (and runtime-data.json), and
// that subdirectory alone is passed to conftest. Anything else living under
// workdir — another phase's runtime data, a git checkout — is therefore
// structurally incapable of being loaded into OPA's data document. Cleanup
// belongs to whoever created workdir; removing it removes these too.
func Run(rendered []byte, ctx trust.Context, policyRoot string, bundleDirs []string, workdir string, runtimeData map[string]any, run ConftestFunc) (Result, error) {
	dataDir, err := os.MkdirTemp(workdir, "data-")
	if err != nil {
		return Result{}, fmt.Errorf("create policy data dir: %w", err)
	}
	if err := writeContext(ctx, dataDir); err != nil {
		return Result{}, err
	}
	if err := writeRuntimeData(runtimeData, dataDir); err != nil {
		return Result{}, err
	}

	args := []string{"test", "--no-color", "--output", "json", "--all-namespaces", "--data", dataDir}
	for _, d := range bundleDirs {
		p := filepath.Join(policyRoot, d)
		args = append(args, "--policy", p, "--data", p)
	}
	args = append(args, "-") // read manifests from stdin

	stdout, err := run(args, rendered)
	if err != nil {
		return Result{}, fmt.Errorf("conftest execution failed (fail-closed): %w", err)
	}

	var parsed []conftestResult
	if err := json.Unmarshal(stdout, &parsed); err != nil {
		return Result{}, fmt.Errorf("parse conftest output (fail-closed): %w; output=%s", err, stdout)
	}

	var res Result
	for _, r := range parsed {
		for _, f := range r.Failures {
			res.Denies = append(res.Denies, Finding{Msg: f.Msg, File: r.Filename})
		}
		for _, w := range r.Warnings {
			res.Warns = append(res.Warns, Finding{Msg: w.Msg, File: r.Filename})
		}
	}
	return res, nil
}

// writeRuntimeData is a no-op for a nil map. The data dir is created fresh per
// invocation, so there is never a stale file from an earlier phase to clear.
func writeRuntimeData(data map[string]any, dataDir string) error {
	if data == nil {
		return nil
	}
	if _, reserved := data["context"]; reserved {
		return fmt.Errorf("runtime data cannot override reserved data.context")
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal runtime data: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "runtime-data.json"), raw, 0o600); err != nil {
		return fmt.Errorf("write runtime-data.json: %w", err)
	}
	return nil
}

func writeContext(ctx trust.Context, dataDir string) error {
	wrapped := map[string]any{"context": ctx}
	data, err := json.MarshalIndent(wrapped, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dataDir, "context.json"), data, 0o644); err != nil {
		return fmt.Errorf("write context.json: %w", err)
	}
	return nil
}
