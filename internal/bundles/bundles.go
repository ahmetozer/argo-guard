// Package bundles loads the guard.yaml registry and selects which policy
// bundles apply to a given trust context.
package bundles

import (
	"fmt"
	"os"

	"github.com/ahmetozer/argo-guard/internal/match"
	"github.com/ahmetozer/argo-guard/internal/trust"
	"gopkg.in/yaml.v3"
)

type Bundle struct {
	Dir     string      `yaml:"dir"`
	Match   match.Expr  `yaml:"match"`
	Exclude *match.Expr `yaml:"exclude"`
}

type Registry struct {
	Bundles []Bundle `yaml:"bundles"`
}

// Load reads and parses a guard.yaml registry file.
func Load(path string) (Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Registry{}, fmt.Errorf("read guard.yaml: %w", err)
	}
	var r Registry
	if err := yaml.Unmarshal(data, &r); err != nil {
		return Registry{}, fmt.Errorf("parse guard.yaml: %w", err)
	}
	for i, b := range r.Bundles {
		if b.Dir == "" {
			return Registry{}, fmt.Errorf("bundle %d: dir is required", i)
		}
	}
	return r, nil
}

// Select returns the dirs of every bundle whose Match holds and whose Exclude
// (if any) does not, preserving registry order. A bundle with match: {} always
// applies.
func (r Registry) Select(c trust.Context) []string {
	var out []string
	for _, b := range r.Bundles {
		if !b.Match.Eval(c) {
			continue
		}
		if b.Exclude != nil && b.Exclude.Eval(c) {
			continue
		}
		out = append(out, b.Dir)
	}
	return out
}
