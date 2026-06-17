package match

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func unmarshalCond(t *testing.T, src string) Condition {
	t.Helper()
	var c Condition
	if err := yaml.Unmarshal([]byte(src), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return c
}

func TestConditionScalarShorthand(t *testing.T) {
	c := unmarshalCond(t, `"team-a"`)
	if !reflect.DeepEqual(c.Equals, []string{"team-a"}) {
		t.Fatalf("got %+v", c)
	}
}

func TestConditionEqualsList(t *testing.T) {
	c := unmarshalCond(t, "equals: [a, b]")
	if !reflect.DeepEqual(c.Equals, []string{"a", "b"}) {
		t.Fatalf("got %+v", c)
	}
}

func TestConditionEqualsScalar(t *testing.T) {
	c := unmarshalCond(t, "equals: a")
	if !reflect.DeepEqual(c.Equals, []string{"a"}) {
		t.Fatalf("got %+v", c)
	}
}

func TestConditionOperators(t *testing.T) {
	c := unmarshalCond(t, "startsWith: https://git.corp/infra/\nendsWith: .git")
	if c.StartsWith != "https://git.corp/infra/" || c.EndsWith != ".git" {
		t.Fatalf("got %+v", c)
	}
}
