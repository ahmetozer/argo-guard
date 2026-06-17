package match

import (
	"testing"

	"github.com/ahmetozer/argo-guard/internal/trust"
	"gopkg.in/yaml.v3"
)

func mustExpr(t *testing.T, src string) Expr {
	t.Helper()
	var e Expr
	if err := yaml.Unmarshal([]byte(src), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return e
}

var infra = trust.Context{
	Repo:      "https://git.corp/infra/platform.git",
	Project:   "platform",
	Namespace: "platform-system",
	AppLabels: map[string]string{"tier": "frontend"},
}

func TestEmptyMatchesAll(t *testing.T) {
	if !(Expr{}).Eval(infra) {
		t.Fatal("empty expr should match")
	}
}

func TestStartsWith(t *testing.T) {
	e := mustExpr(t, "repo: { startsWith: https://git.corp/infra/ }")
	if !e.Eval(infra) {
		t.Fatal("should match infra repo prefix")
	}
	other := infra
	other.Repo = "https://git.corp/team-a/app.git"
	if e.Eval(other) {
		t.Fatal("should not match non-infra repo")
	}
}

func TestImplicitAndAcrossFields(t *testing.T) {
	e := mustExpr(t, "project: platform\nnamespace: { like: 'platform-*' }")
	if !e.Eval(infra) {
		t.Fatal("both conditions hold; should match")
	}
	other := infra
	other.Project = "team-a"
	if e.Eval(other) {
		t.Fatal("project differs; should not match")
	}
}

func TestOrNode(t *testing.T) {
	e := mustExpr(t, "or:\n  - namespace: { like: 'platform-*' }\n  - label: { tier: { equals: backend } }")
	if !e.Eval(infra) {
		t.Fatal("first or-branch matches; should match")
	}
}

func TestExcludeStyleNotEquals(t *testing.T) {
	e := mustExpr(t, "namespace: { equals: [kube-system, argocd] }")
	if e.Eval(infra) {
		t.Fatal("platform-system is not in the list")
	}
	ks := infra
	ks.Namespace = "kube-system"
	if !e.Eval(ks) {
		t.Fatal("kube-system is in the list")
	}
}

func TestLabel(t *testing.T) {
	e := mustExpr(t, "label: { tier: frontend }")
	if !e.Eval(infra) {
		t.Fatal("label tier=frontend should match")
	}
}

func TestGlobCrossesSlashes(t *testing.T) {
	c := Condition{Like: "https://git.corp/infra/*"}
	if !c.match("https://git.corp/infra/a/b.git") {
		t.Fatal("* should cross slashes")
	}
}
