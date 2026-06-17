// Package match implements the declarative selection DSL used in guard.yaml to
// route policy bundles to applications. It operates only on the trust context.
package match

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Condition is a set of operators on a single string field. All present
// operators must hold (AND). An empty Condition matches anything.
type Condition struct {
	Equals        []string
	NotEquals     []string
	Like          string
	NotLike       string
	StartsWith    string
	NotStartsWith string
	EndsWith      string
	NotEndsWith   string
}

// stringOrSlice accepts either a scalar or a sequence of scalars.
type stringOrSlice []string

func (s *stringOrSlice) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*s = stringOrSlice{node.Value}
		return nil
	case yaml.SequenceNode:
		var out []string
		if err := node.Decode(&out); err != nil {
			return err
		}
		*s = out
		return nil
	default:
		return fmt.Errorf("expected scalar or sequence, got kind %d", node.Kind)
	}
}

func (c *Condition) UnmarshalYAML(node *yaml.Node) error {
	// Shorthand: a bare scalar means equals.
	if node.Kind == yaml.ScalarNode {
		c.Equals = []string{node.Value}
		return nil
	}
	var aux struct {
		Equals        stringOrSlice `yaml:"equals"`
		NotEquals     stringOrSlice `yaml:"notEquals"`
		Like          string        `yaml:"like"`
		NotLike       string        `yaml:"notLike"`
		StartsWith    string        `yaml:"startsWith"`
		NotStartsWith string        `yaml:"notStartsWith"`
		EndsWith      string        `yaml:"endsWith"`
		NotEndsWith   string        `yaml:"notEndsWith"`
	}
	if err := node.Decode(&aux); err != nil {
		return err
	}
	c.Equals = aux.Equals
	c.NotEquals = aux.NotEquals
	c.Like = aux.Like
	c.NotLike = aux.NotLike
	c.StartsWith = aux.StartsWith
	c.NotStartsWith = aux.NotStartsWith
	c.EndsWith = aux.EndsWith
	c.NotEndsWith = aux.NotEndsWith
	return nil
}
