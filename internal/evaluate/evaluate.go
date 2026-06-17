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

// Run evaluates rendered against the selected bundle policy dirs. workdir is a
// scratch dir where context.json is written and passed to conftest via --data.
func Run(rendered []byte, ctx trust.Context, policyRoot string, bundleDirs []string, workdir string, run ConftestFunc) (Result, error) {
	if err := writeContext(ctx, workdir); err != nil {
		return Result{}, err
	}

	args := []string{"test", "--no-color", "--output", "json", "--all-namespaces", "--data", workdir}
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

func writeContext(ctx trust.Context, workdir string) error {
	wrapped := map[string]any{"context": ctx}
	data, err := json.MarshalIndent(wrapped, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workdir, "context.json"), data, 0o644); err != nil {
		return fmt.Errorf("write context.json: %w", err)
	}
	return nil
}
