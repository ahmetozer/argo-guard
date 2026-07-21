// Package policyflat expands a flattened policy directory — a ConfigMap
// volume mount whose keys encode paths with "__" (global__restrictions.rego
// -> global/restrictions.rego) — into a real directory tree that guard.yaml
// bundle dirs can resolve against.
package policyflat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Mode parses GUARD_POLICY_FLAT. Empty means off; "true" means on; any other
// value is a configuration error (fail-closed).
func Mode(v string) (bool, error) {
	switch v {
	case "":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("GUARD_POLICY_FLAT must be unset or \"true\", got %q", v)
	}
}

// Expand copies every file in srcDir into dstDir, mapping "__" in filenames
// to path separators. Entries starting with "." (kubelet's ..data and
// timestamped bookkeeping dirs in ConfigMap mounts) are skipped; symlinked
// files are followed. Any irregularity fails closed with an error.
func Expand(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read flat policy dir: %w", err)
	}
	expanded := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		src := filepath.Join(srcDir, name)
		info, err := os.Stat(src) // follows the mount's top-level symlinks
		if err != nil {
			return fmt.Errorf("stat %q: %w", name, err)
		}
		if info.IsDir() {
			return fmt.Errorf("unexpected directory %q in flat policy dir (fail-closed)", name)
		}
		rel, err := unflatten(name)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstDir, rel)
		if fi, err := os.Stat(dst); err == nil && fi.IsDir() {
			return fmt.Errorf("flattened key %q collides with a directory created by another key", name)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("flattened key %q collides with another key: %w", name, err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read %q: %w", name, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("write %q: %w", rel, err)
		}
		expanded++
	}
	if expanded == 0 {
		return fmt.Errorf("flat policy dir %s contains no files (fail-closed)", srcDir)
	}
	return nil
}

// unflatten maps a flattened key to a relative path and validates it.
func unflatten(name string) (string, error) {
	rel := strings.ReplaceAll(name, "__", "/")
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("invalid flattened key %q: maps to unsafe path %q", name, rel)
		}
	}
	return rel, nil
}
