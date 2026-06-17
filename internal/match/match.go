package match

import (
	"regexp"
	"strings"

	"github.com/ahmetozer/argo-guard/internal/trust"
)

// Expr is a match expression node. Fields present in one Expr combine with AND.
// And/Or provide explicit logical composition and may nest.
type Expr struct {
	And       []Expr               `yaml:"and"`
	Or        []Expr               `yaml:"or"`
	Repo      *Condition           `yaml:"repo"`
	Project   *Condition           `yaml:"project"`
	Namespace *Condition           `yaml:"namespace"`
	Label     map[string]Condition `yaml:"label"`
}

// Eval reports whether the context satisfies this expression. An empty Expr
// (e.g. `match: {}`) matches everything.
func (e Expr) Eval(c trust.Context) bool {
	for _, sub := range e.And {
		if !sub.Eval(c) {
			return false
		}
	}
	if len(e.Or) > 0 {
		any := false
		for _, sub := range e.Or {
			if sub.Eval(c) {
				any = true
				break
			}
		}
		if !any {
			return false
		}
	}
	if e.Repo != nil && !e.Repo.match(c.Repo) {
		return false
	}
	if e.Project != nil && !e.Project.match(c.Project) {
		return false
	}
	if e.Namespace != nil && !e.Namespace.match(c.Namespace) {
		return false
	}
	for key, cond := range e.Label {
		if !cond.match(c.AppLabels[key]) {
			return false
		}
	}
	return true
}

// match reports whether value satisfies every operator set on the condition.
func (c Condition) match(value string) bool {
	if len(c.Equals) > 0 && !contains(c.Equals, value) {
		return false
	}
	if len(c.NotEquals) > 0 && contains(c.NotEquals, value) {
		return false
	}
	if c.Like != "" && !globMatch(c.Like, value) {
		return false
	}
	if c.NotLike != "" && globMatch(c.NotLike, value) {
		return false
	}
	if c.StartsWith != "" && !strings.HasPrefix(value, c.StartsWith) {
		return false
	}
	if c.NotStartsWith != "" && strings.HasPrefix(value, c.NotStartsWith) {
		return false
	}
	if c.EndsWith != "" && !strings.HasSuffix(value, c.EndsWith) {
		return false
	}
	if c.NotEndsWith != "" && strings.HasSuffix(value, c.NotEndsWith) {
		return false
	}
	return true
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// globMatch treats * as any run of characters (including '/') and ? as one.
func globMatch(pattern, s string) bool {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String()).MatchString(s)
}
