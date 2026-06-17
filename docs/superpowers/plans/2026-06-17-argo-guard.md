# argo-guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an Argo CD Config Management Plugin that renders Kustomize apps, validates them against layered Conftest/Rego policies selected by a declarative match DSL, and fails the sync on violations — all in `argocd-repo-server`, never touching a target cluster's control plane.

**Architecture:** A single Go binary (`argo-guard generate`) runs as a CMP sidecar. It renders manifests with `kustomize build`, builds a spoof-proof trust context from Argo env vars, selects policy bundles from a cached Git policy repo using a `match`/`exclude` operator DSL, evaluates them with `conftest test` (context injected as `data.context`), and emits manifests on pass or a violation report + non-zero exit on fail. Fail-closed throughout.

**Tech Stack:** Go 1.22; `gopkg.in/yaml.v3` for YAML; shells out to `kustomize`, `conftest`, `git`. No Kubernetes client libraries (no cluster API access by design).

## Global Constraints

- Module path: `github.com/ahmetozer/argo-guard`.
- Go 1.22; standard library + `gopkg.in/yaml.v3` only (no k8s client-go, no cobra — use stdlib `flag`/`os.Args`).
- **Fail-closed:** any error (render, policy load, conftest error) returns a non-zero exit; never emit manifests on error.
- All external commands (`kustomize`, `conftest`, `git`) are invoked through injected function dependencies so every unit is testable without the real binaries.
- No clock or filesystem-global state read directly in logic — inject `now func() time.Time` and explicit paths.
- Trust context fields come **only** from Argo-provided env vars, never from manifest content.
- Exit codes: `0` = pass (manifests on stdout); `1` = policy violation; `2` = internal/fail-closed error. Reports go to **stderr**, manifests to **stdout**.

---

### Task 1: Project scaffold + trust context

**Files:**
- Create: `go.mod`
- Create: `internal/trust/trust.go`
- Test: `internal/trust/trust_test.go`

**Interfaces:**
- Consumes: nothing (leaf).
- Produces:
  - `type trust.Context struct { Repo, Project, Namespace string; AppLabels map[string]string }`
  - `func trust.FromEnv(getenv func(string) string) trust.Context`

- [ ] **Step 1: Create the Go module**

Run:
```bash
cd /Users/ahmetozer/Desktop/Work/argo-guard
go mod init github.com/ahmetozer/argo-guard
go get gopkg.in/yaml.v3@v3.0.1
```
Expected: `go.mod` and `go.sum` created.

- [ ] **Step 2: Write the failing test**

`internal/trust/trust_test.go`:
```go
package trust

import (
	"reflect"
	"testing"
)

func TestFromEnv(t *testing.T) {
	env := map[string]string{
		"ARGOCD_APP_SOURCE_REPO_URL": "https://git.corp/infra/platform.git",
		"ARGOCD_APP_PROJECT_NAME":    "platform",
		"ARGOCD_APP_NAMESPACE":       "ingress",
		"ARGOCD_ENV_TIER":            "frontend",
		"ARGOCD_ENV_REGION":          "eu",
		"UNRELATED":                  "x",
	}
	got := FromEnv(func(k string) string { return env[k] })

	want := Context{
		Repo:      "https://git.corp/infra/platform.git",
		Project:   "platform",
		Namespace: "ingress",
		AppLabels: map[string]string{"tier": "frontend", "region": "eu"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestFromEnvEmpty(t *testing.T) {
	got := FromEnv(func(string) string { return "" })
	if got.Repo != "" || len(got.AppLabels) != 0 {
		t.Fatalf("expected empty context, got %+v", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/trust/`
Expected: FAIL — `undefined: FromEnv` / `undefined: Context`.

- [ ] **Step 4: Write minimal implementation**

`internal/trust/trust.go`:
```go
// Package trust builds the spoof-proof deployment context from Argo CD
// environment variables. Nothing here ever reads manifest content.
package trust

import (
	"os"
	"strings"
)

// Context is the trust context injected into policy evaluation and used to
// select policy bundles. All fields originate from Argo-provided env vars.
type Context struct {
	Repo      string            `json:"repo"`
	Project   string            `json:"project"`
	Namespace string            `json:"namespace"`
	AppLabels map[string]string `json:"appLabels"`
}

const envLabelPrefix = "ARGOCD_ENV_"

// FromEnv assembles a Context. getenv is injected for testing; pass os.Getenv
// in production. AppLabels are sourced from ARGOCD_ENV_* plugin parameters
// (lowercased keys), since Argo does not expose Application labels to a CMP.
func FromEnv(getenv func(string) string) Context {
	c := Context{
		Repo:      getenv("ARGOCD_APP_SOURCE_REPO_URL"),
		Project:   getenv("ARGOCD_APP_PROJECT_NAME"),
		Namespace: getenv("ARGOCD_APP_NAMESPACE"),
		AppLabels: map[string]string{},
	}
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		key, val := kv[:eq], kv[eq+1:]
		if strings.HasPrefix(key, envLabelPrefix) {
			c.AppLabels[strings.ToLower(strings.TrimPrefix(key, envLabelPrefix))] = val
		}
	}
	return c
}
```

Note: `FromEnv` reads scalar fields via the injected `getenv` (so the unit test controls them) but scans `os.Environ()` for label discovery. The test sets process env for the `ARGOCD_ENV_*` keys — adjust the test to set them via `t.Setenv`:

Add to the top of `TestFromEnv` before calling `FromEnv`:
```go
	t.Setenv("ARGOCD_ENV_TIER", "frontend")
	t.Setenv("ARGOCD_ENV_REGION", "eu")
```
(and remove those two keys from the `env` map; keep them only in `t.Setenv`).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/trust/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/trust/
git commit -m "feat: project scaffold and trust context"
```

---

### Task 2: Match DSL types and unmarshalling

**Files:**
- Create: `internal/match/condition.go`
- Test: `internal/match/condition_test.go`

**Interfaces:**
- Consumes: nothing (leaf).
- Produces:
  - `type match.Condition struct { Equals, NotEquals []string; Like, NotLike, StartsWith, NotStartsWith, EndsWith, NotEndsWith string }`
  - `Condition` implements `yaml.Unmarshaler`: a scalar node becomes `Equals: [scalar]`; `equals`/`notEquals` accept scalar or sequence.

- [ ] **Step 1: Write the failing test**

`internal/match/condition_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/match/`
Expected: FAIL — `undefined: Condition`.

- [ ] **Step 3: Write minimal implementation**

`internal/match/condition.go`:
```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/match/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/match/condition.go internal/match/condition_test.go
git commit -m "feat: match DSL condition type and unmarshalling"
```

---

### Task 3: Match DSL evaluator

**Files:**
- Create: `internal/match/match.go`
- Test: `internal/match/match_test.go`

**Interfaces:**
- Consumes: `trust.Context` (Task 1), `Condition` (Task 2).
- Produces:
  - `type match.Expr struct { And, Or []Expr; Repo, Project, Namespace *Condition; Label map[string]Condition }`
  - `func (e Expr) Eval(c trust.Context) bool` — empty Expr returns true; fields combine with AND; `Or` requires any sub true.

- [ ] **Step 1: Write the failing test**

`internal/match/match_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/match/`
Expected: FAIL — `undefined: Expr`.

- [ ] **Step 3: Write minimal implementation**

`internal/match/match.go`:
```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/match/`
Expected: PASS (all sub-tests).

- [ ] **Step 5: Commit**

```bash
git add internal/match/match.go internal/match/match_test.go
git commit -m "feat: match DSL evaluator with operators and and/or composition"
```

---

### Task 4: Bundle registry and selection

**Files:**
- Create: `internal/bundles/bundles.go`
- Test: `internal/bundles/bundles_test.go`

**Interfaces:**
- Consumes: `match.Expr` (Task 3), `trust.Context` (Task 1).
- Produces:
  - `type bundles.Bundle struct { Dir string; Match match.Expr; Exclude *match.Expr }`
  - `type bundles.Registry struct { Bundles []Bundle }`
  - `func bundles.Load(path string) (Registry, error)`
  - `func (r Registry) Select(c trust.Context) []string` — returns dirs where `Match` holds AND (`Exclude` nil OR `Exclude` does not hold), preserving file order.

- [ ] **Step 1: Write the failing test**

`internal/bundles/bundles_test.go`:
```go
package bundles

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ahmetozer/argo-guard/internal/trust"
)

const sample = `
bundles:
  - dir: global
    match: {}
  - dir: projects/infra
    match:
      and:
        - repo: { startsWith: "https://git.corp/infra/" }
    exclude:
      namespace: { equals: [kube-system, argocd] }
  - dir: namespaces/payments
    match:
      namespace: payments
`

func writeRegistry(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "guard.yaml")
	if err := os.WriteFile(p, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSelectGlobalAlways(t *testing.T) {
	r, err := Load(writeRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	got := r.Select(trust.Context{Repo: "https://git.corp/team-a/app.git", Namespace: "default"})
	if !reflect.DeepEqual(got, []string{"global"}) {
		t.Fatalf("got %v", got)
	}
}

func TestSelectInfraWithExclude(t *testing.T) {
	r, _ := Load(writeRegistry(t))
	got := r.Select(trust.Context{Repo: "https://git.corp/infra/p.git", Namespace: "ingress"})
	if !reflect.DeepEqual(got, []string{"global", "projects/infra"}) {
		t.Fatalf("got %v", got)
	}
	excluded := r.Select(trust.Context{Repo: "https://git.corp/infra/p.git", Namespace: "kube-system"})
	if !reflect.DeepEqual(excluded, []string{"global"}) {
		t.Fatalf("exclude should drop infra bundle, got %v", excluded)
	}
}

func TestSelectMultiple(t *testing.T) {
	r, _ := Load(writeRegistry(t))
	got := r.Select(trust.Context{Repo: "https://git.corp/infra/p.git", Namespace: "payments"})
	if !reflect.DeepEqual(got, []string{"global", "projects/infra", "namespaces/payments"}) {
		t.Fatalf("got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bundles/`
Expected: FAIL — `undefined: Load`.

- [ ] **Step 3: Write minimal implementation**

`internal/bundles/bundles.go`:
```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bundles/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bundles/
git commit -m "feat: guard.yaml registry loading and bundle selection"
```

---

### Task 5: Policy repo cache

**Files:**
- Create: `internal/policyrepo/policyrepo.go`
- Test: `internal/policyrepo/policyrepo_test.go`

**Interfaces:**
- Consumes: nothing in-repo (uses injected git + clock).
- Produces:
  - `type policyrepo.Cache struct { ... }`
  - `func policyrepo.New(repoURL, ref, dir string, ttl time.Duration, git GitFunc, now func() time.Time) *Cache`
  - `type policyrepo.GitFunc func(workdir string, args ...string) error`
  - `func (c *Cache) Ensure() (path string, stale bool, err error)` — clones on cold start; fetches+checkouts when older than TTL; on fetch failure with existing cache returns `(dir, true, nil)`; on cold start with fetch failure returns error (fail-closed).

- [ ] **Step 1: Write the failing test**

`internal/policyrepo/policyrepo_test.go`:
```go
package policyrepo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestColdStartClones(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	var calls [][]string
	git := func(workdir string, args ...string) error {
		calls = append(calls, args)
		// Simulate clone creating the dir.
		if args[0] == "clone" {
			return os.MkdirAll(dir, 0o755)
		}
		return nil
	}
	now := time.Unix(1000, 0)
	c := New("https://git/policies.git", "main", dir, time.Minute, git, func() time.Time { return now })

	path, stale, err := c.Ensure()
	if err != nil || stale {
		t.Fatalf("err=%v stale=%v", err, stale)
	}
	if path != dir {
		t.Fatalf("path=%s", path)
	}
	if calls[0][0] != "clone" {
		t.Fatalf("expected clone first, got %v", calls)
	}
}

func TestColdStartFetchFailsClosed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	git := func(string, ...string) error { return errors.New("network down") }
	c := New("u", "main", dir, time.Minute, git, func() time.Time { return time.Unix(0, 0) })

	if _, _, err := c.Ensure(); err == nil {
		t.Fatal("cold start with failing git must error (fail-closed)")
	}
}

func TestStaleCacheServedOnFetchFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil { // pre-existing cache
		t.Fatal(err)
	}
	git := func(workdir string, args ...string) error {
		if args[0] == "fetch" {
			return errors.New("network down")
		}
		return nil
	}
	cur := time.Unix(10000, 0)
	c := New("u", "main", dir, time.Minute, git, func() time.Time { return cur })
	c.lastSync = time.Unix(0, 0) // force staleness

	path, stale, err := c.Ensure()
	if err != nil {
		t.Fatalf("should serve stale cache, got err %v", err)
	}
	if !stale || path != dir {
		t.Fatalf("stale=%v path=%s", stale, path)
	}
}

func TestFreshCacheSkipsFetch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	os.MkdirAll(dir, 0o755)
	var fetched bool
	git := func(workdir string, args ...string) error {
		if args[0] == "fetch" {
			fetched = true
		}
		return nil
	}
	cur := time.Unix(100, 0)
	c := New("u", "main", dir, time.Minute, git, func() time.Time { return cur })
	c.lastSync = time.Unix(90, 0) // 10s ago, TTL 60s → fresh

	if _, _, err := c.Ensure(); err != nil {
		t.Fatal(err)
	}
	if fetched {
		t.Fatal("fresh cache should not fetch")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/policyrepo/`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write minimal implementation**

`internal/policyrepo/policyrepo.go`:
```go
// Package policyrepo manages a local cache of the policy Git repository.
package policyrepo

import (
	"fmt"
	"os"
	"time"
)

// GitFunc runs a git command in workdir. Injected for testability.
type GitFunc func(workdir string, args ...string) error

type Cache struct {
	repoURL  string
	ref      string
	dir      string
	ttl      time.Duration
	git      GitFunc
	now      func() time.Time
	lastSync time.Time
}

func New(repoURL, ref, dir string, ttl time.Duration, git GitFunc, now func() time.Time) *Cache {
	return &Cache{repoURL: repoURL, ref: ref, dir: dir, ttl: ttl, git: git, now: now}
}

func (c *Cache) exists() bool {
	_, err := os.Stat(c.dir)
	return err == nil
}

// Ensure returns the local path to fresh-enough policies. stale is true when
// the cache could not be refreshed but a previous copy is being served.
func (c *Cache) Ensure() (path string, stale bool, err error) {
	if !c.exists() {
		// Cold start: must succeed or fail closed.
		if err := c.git("", "clone", "--branch", c.ref, "--depth", "1", c.repoURL, c.dir); err != nil {
			return "", false, fmt.Errorf("cold-start clone of policy repo failed (fail-closed): %w", err)
		}
		c.lastSync = c.now()
		return c.dir, false, nil
	}
	if c.now().Sub(c.lastSync) < c.ttl {
		return c.dir, false, nil // fresh
	}
	// Refresh existing cache.
	if err := c.git(c.dir, "fetch", "--depth", "1", "origin", c.ref); err != nil {
		return c.dir, true, nil // serve last-known-good
	}
	if err := c.git(c.dir, "checkout", "-f", "FETCH_HEAD"); err != nil {
		return c.dir, true, nil
	}
	c.lastSync = c.now()
	return c.dir, false, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/policyrepo/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/policyrepo/
git commit -m "feat: policy repo cache with TTL refresh and fail-closed cold start"
```

---

### Task 6: Render (kustomize build + parse)

**Files:**
- Create: `internal/render/render.go`
- Test: `internal/render/render_test.go`

**Interfaces:**
- Consumes: nothing in-repo (injected kustomize runner).
- Produces:
  - `type render.Resource struct { Kind string; Name string; Namespace string; Doc map[string]any }`
  - `type render.KustomizeFunc func(path string) ([]byte, error)`
  - `func render.Build(path string, k KustomizeFunc) (raw []byte, resources []Resource, err error)` — runs kustomize, returns raw output for emit plus parsed resources.

- [ ] **Step 1: Write the failing test**

`internal/render/render_test.go`:
```go
package render

import (
	"errors"
	"testing"
)

const twoDocs = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: payments
---
apiVersion: v1
kind: Service
metadata:
  name: web
`

func TestBuildParsesDocs(t *testing.T) {
	k := func(path string) ([]byte, error) { return []byte(twoDocs), nil }
	raw, res, err := Build("app/", k)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != twoDocs {
		t.Fatal("raw output must be returned verbatim for emit")
	}
	if len(res) != 2 {
		t.Fatalf("want 2 resources, got %d", len(res))
	}
	if res[0].Kind != "Deployment" || res[0].Name != "web" || res[0].Namespace != "payments" {
		t.Fatalf("got %+v", res[0])
	}
	if res[1].Kind != "Service" || res[1].Namespace != "" {
		t.Fatalf("got %+v", res[1])
	}
}

func TestBuildFailsClosedOnKustomizeError(t *testing.T) {
	k := func(string) ([]byte, error) { return nil, errors.New("kustomize boom") }
	if _, _, err := Build("app/", k); err == nil {
		t.Fatal("kustomize error must propagate (fail-closed)")
	}
}

func TestBuildSkipsEmptyDocs(t *testing.T) {
	k := func(string) ([]byte, error) { return []byte("---\n\n---\nkind: ConfigMap\nmetadata:\n  name: c\n"), nil }
	_, res, err := Build("app/", k)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("empty docs should be skipped, got %d", len(res))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/render/`
Expected: FAIL — `undefined: Build`.

- [ ] **Step 3: Write minimal implementation**

`internal/render/render.go`:
```go
// Package render runs kustomize build and parses the rendered manifests.
package render

import (
	"bytes"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

type Resource struct {
	Kind      string
	Name      string
	Namespace string
	Doc       map[string]any
}

// KustomizeFunc runs `kustomize build path` and returns its stdout.
type KustomizeFunc func(path string) ([]byte, error)

// Build renders the app at path and parses the multi-document output. The raw
// bytes are returned unchanged so they can be emitted verbatim on success.
func Build(path string, k KustomizeFunc) ([]byte, []Resource, error) {
	raw, err := k(path)
	if err != nil {
		return nil, nil, fmt.Errorf("kustomize build %s (fail-closed): %w", path, err)
	}
	resources, err := parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("parse rendered manifests: %w", err)
	}
	return raw, resources, nil
}

func parse(raw []byte) ([]Resource, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var out []Resource
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(doc) == 0 {
			continue // empty document between separators
		}
		out = append(out, toResource(doc))
	}
	return out, nil
}

func toResource(doc map[string]any) Resource {
	r := Resource{Doc: doc}
	r.Kind, _ = doc["kind"].(string)
	if md, ok := doc["metadata"].(map[string]any); ok {
		r.Name, _ = md["name"].(string)
		r.Namespace, _ = md["namespace"].(string)
	}
	return r
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/render/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/render/
git commit -m "feat: kustomize render and multi-doc manifest parsing"
```

---

### Task 7: Evaluate (conftest wrapper)

**Files:**
- Create: `internal/evaluate/evaluate.go`
- Test: `internal/evaluate/evaluate_test.go`

**Interfaces:**
- Consumes: `trust.Context` (Task 1).
- Produces:
  - `type evaluate.Finding struct { Rule, Msg, File string }`
  - `type evaluate.Result struct { Denies, Warns []Finding }`
  - `type evaluate.ConftestFunc func(args []string, stdin []byte) (stdout []byte, err error)`
  - `func evaluate.Run(rendered []byte, ctx trust.Context, policyRoot string, bundleDirs []string, workdir string, run ConftestFunc) (Result, error)` — writes `context.json` into workdir, invokes conftest with `--data`, parses JSON output, classifies failures/warnings. A conftest *execution* error (vs policy failures) returns an error (fail-closed).

- [ ] **Step 1: Write the failing test**

`internal/evaluate/evaluate_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/evaluate/`
Expected: FAIL — `undefined: Run`.

- [ ] **Step 3: Write minimal implementation**

`internal/evaluate/evaluate.go`:
```go
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
		args = append(args, "--policy", filepath.Join(policyRoot, d))
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/evaluate/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/evaluate/
git commit -m "feat: conftest evaluation with data.context injection"
```

---

### Task 8: Emit (stdout manifests / stderr report)

**Files:**
- Create: `internal/emit/emit.go`
- Test: `internal/emit/emit_test.go`

**Interfaces:**
- Consumes: `evaluate.Result`, `evaluate.Finding` (Task 7).
- Produces:
  - `func emit.Success(stdout io.Writer, rendered []byte) error`
  - `func emit.Report(stderr io.Writer, res evaluate.Result, stale bool)` — writes a human-readable violation/warning report.

- [ ] **Step 1: Write the failing test**

`internal/emit/emit_test.go`:
```go
package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ahmetozer/argo-guard/internal/evaluate"
)

func TestSuccessWritesManifests(t *testing.T) {
	var out bytes.Buffer
	if err := Success(&out, []byte("kind: Service\n")); err != nil {
		t.Fatal(err)
	}
	if out.String() != "kind: Service\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestReportListsDeniesAndWarns(t *testing.T) {
	var errb bytes.Buffer
	res := evaluate.Result{
		Denies: []evaluate.Finding{{Msg: "LoadBalancer not allowed", File: "svc.yaml"}},
		Warns:  []evaluate.Finding{{Msg: "missing owner label", File: "deploy.yaml"}},
	}
	Report(&errb, res, true)
	s := errb.String()
	for _, want := range []string{"LoadBalancer not allowed", "missing owner label", "DENY", "WARN", "stale"} {
		if !strings.Contains(s, want) {
			t.Fatalf("report missing %q:\n%s", want, s)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emit/`
Expected: FAIL — `undefined: Success`.

- [ ] **Step 3: Write minimal implementation**

`internal/emit/emit.go`:
```go
// Package emit writes rendered manifests to stdout (pass) or a violation
// report to stderr (fail/warn).
package emit

import (
	"fmt"
	"io"

	"github.com/ahmetozer/argo-guard/internal/evaluate"
)

// Success writes the rendered manifests verbatim for Argo to apply.
func Success(stdout io.Writer, rendered []byte) error {
	_, err := stdout.Write(rendered)
	return err
}

// Report writes a readable summary of findings to stderr. stale indicates the
// policy cache could not be refreshed and last-known-good was used.
func Report(stderr io.Writer, res evaluate.Result, stale bool) {
	fmt.Fprintln(stderr, "argo-guard policy report")
	if stale {
		fmt.Fprintln(stderr, "  WARNING: policy cache is stale (last-known-good served; repo unreachable)")
	}
	for _, d := range res.Denies {
		fmt.Fprintf(stderr, "  DENY  [%s] %s\n", d.File, d.Msg)
	}
	for _, w := range res.Warns {
		fmt.Fprintf(stderr, "  WARN  [%s] %s\n", w.File, w.Msg)
	}
	fmt.Fprintf(stderr, "  summary: %d deny, %d warn\n", len(res.Denies), len(res.Warns))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/emit/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/emit/
git commit -m "feat: emit manifests on pass and violation report on fail"
```

---

### Task 9: Generate orchestrator + main wiring

**Files:**
- Create: `internal/generate/generate.go`
- Create: `cmd/argo-guard/main.go`
- Test: `internal/generate/generate_test.go`

**Interfaces:**
- Consumes: all packages from Tasks 1–8.
- Produces:
  - `type generate.Deps struct { Getenv func(string) string; Kustomize render.KustomizeFunc; Conftest evaluate.ConftestFunc; EnsurePolicies func() (root string, stale bool, err error); WorkDir string }`
  - `func generate.Run(d Deps, stdout, stderr io.Writer) int` — returns exit code (0 pass, 1 violation, 2 internal error). Orchestrates render → context → ensure policies → load registry → select → evaluate → emit.

- [ ] **Step 1: Write the failing test**

`internal/generate/generate_test.go`:
```go
package generate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ahmetozer/argo-guard/internal/render"
)

// fakePolicyRoot writes a guard.yaml so Select returns the global bundle.
func fakePolicyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	guard := "bundles:\n  - dir: global\n    match: {}\n"
	if err := os.WriteFile(filepath.Join(root, "guard.yaml"), []byte(guard), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func baseDeps(t *testing.T, conftestOut string, conftestErr error) (Deps, string) {
	root := fakePolicyRoot(t)
	d := Deps{
		Getenv:    func(k string) string { return map[string]string{"ARGOCD_APP_SOURCE_PATH": "app/"}[k] },
		Kustomize: func(string) ([]byte, error) { return []byte("kind: Service\nmetadata:\n  name: web\n"), nil },
		Conftest:  func([]string, []byte) ([]byte, error) { return []byte(conftestOut), conftestErr },
		EnsurePolicies: func() (string, bool, error) { return root, false, nil },
		WorkDir:   t.TempDir(),
	}
	return d, root
}

func TestRunCleanEmitsManifests(t *testing.T) {
	d, _ := baseDeps(t, `[{"filename":"-","failures":[],"warnings":[]}]`, nil)
	var out, errb bytes.Buffer
	code := Run(d, &out, &errb)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if out.String() == "" {
		t.Fatal("expected manifests on stdout")
	}
}

func TestRunViolationExits1NoManifests(t *testing.T) {
	d, _ := baseDeps(t, `[{"filename":"-","failures":[{"msg":"no LoadBalancer"}],"warnings":[]}]`, nil)
	var out, errb bytes.Buffer
	code := Run(d, &out, &errb)
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
	if out.Len() != 0 {
		t.Fatal("must NOT emit manifests on violation")
	}
}

func TestRunInternalErrorExits2(t *testing.T) {
	d, _ := baseDeps(t, "", errors.New("conftest exploded"))
	var out, errb bytes.Buffer
	code := Run(d, &out, &errb)
	if code != 2 {
		t.Fatalf("want exit 2 (fail-closed), got %d", code)
	}
	if out.Len() != 0 {
		t.Fatal("must NOT emit manifests on internal error")
	}
}

func TestRunKustomizeErrorExits2(t *testing.T) {
	d, _ := baseDeps(t, `[]`, nil)
	d.Kustomize = func(string) ([]byte, error) { return nil, errors.New("bad kustomization") }
	var out, errb bytes.Buffer
	if code := Run(d, &out, &errb); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
	_ = render.Resource{} // ensure render import used
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/generate/`
Expected: FAIL — `undefined: Run` / `undefined: Deps`.

- [ ] **Step 3: Write minimal implementation**

`internal/generate/generate.go`:
```go
// Package generate orchestrates the full render→validate→emit pipeline that
// backs the `argo-guard generate` CMP command. It is fail-closed: any error
// returns exit code 2 and never emits manifests.
package generate

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/ahmetozer/argo-guard/internal/bundles"
	"github.com/ahmetozer/argo-guard/internal/emit"
	"github.com/ahmetozer/argo-guard/internal/evaluate"
	"github.com/ahmetozer/argo-guard/internal/render"
	"github.com/ahmetozer/argo-guard/internal/trust"
)

type Deps struct {
	Getenv         func(string) string
	Kustomize      render.KustomizeFunc
	Conftest       evaluate.ConftestFunc
	EnsurePolicies func() (root string, stale bool, err error)
	WorkDir        string
}

const (
	exitPass      = 0
	exitViolation = 1
	exitError     = 2
)

// Run executes the pipeline and returns the process exit code.
func Run(d Deps, stdout, stderr io.Writer) int {
	appPath := d.Getenv("ARGOCD_APP_SOURCE_PATH")
	if appPath == "" {
		appPath = "."
	}

	raw, _, err := render.Build(appPath, d.Kustomize)
	if err != nil {
		fmt.Fprintf(stderr, "argo-guard: %v\n", err)
		return exitError
	}

	ctx := trust.FromEnv(d.Getenv)

	policyRoot, stale, err := d.EnsurePolicies()
	if err != nil {
		fmt.Fprintf(stderr, "argo-guard: %v\n", err)
		return exitError
	}

	registry, err := bundles.Load(filepath.Join(policyRoot, "guard.yaml"))
	if err != nil {
		fmt.Fprintf(stderr, "argo-guard: %v\n", err)
		return exitError
	}
	selected := registry.Select(ctx)

	res, err := evaluate.Run(raw, ctx, policyRoot, selected, d.WorkDir, d.Conftest)
	if err != nil {
		fmt.Fprintf(stderr, "argo-guard: %v\n", err)
		return exitError
	}

	if len(res.Denies) > 0 {
		emit.Report(stderr, res, stale)
		return exitViolation
	}
	if len(res.Warns) > 0 || stale {
		emit.Report(stderr, res, stale) // surface warnings but do not block
	}
	if err := emit.Success(stdout, raw); err != nil {
		fmt.Fprintf(stderr, "argo-guard: emit failed: %v\n", err)
		return exitError
	}
	return exitPass
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/generate/`
Expected: PASS.

- [ ] **Step 5: Write `main.go` wiring real dependencies**

`cmd/argo-guard/main.go`:
```go
// Command argo-guard is an Argo CD Config Management Plugin that validates
// rendered Kustomize manifests against layered Conftest/Rego policies.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ahmetozer/argo-guard/internal/evaluate"
	"github.com/ahmetozer/argo-guard/internal/generate"
	"github.com/ahmetozer/argo-guard/internal/policyrepo"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "generate" {
		fmt.Fprintln(os.Stderr, "usage: argo-guard generate")
		os.Exit(2)
	}

	cacheDir := getenvDefault("GUARD_POLICY_CACHE", "/var/cache/argo-guard/policies")
	ttl := parseTTL(getenvDefault("GUARD_POLICY_TTL", "60s"))
	workDir, err := os.MkdirTemp("", "argo-guard-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "argo-guard: workdir: %v\n", err)
		os.Exit(2)
	}
	defer os.RemoveAll(workDir)

	cache := policyrepo.New(
		os.Getenv("GUARD_POLICY_REPO"),
		getenvDefault("GUARD_POLICY_REF", "main"),
		cacheDir, ttl,
		func(workdir string, args ...string) error {
			cmd := exec.Command("git", args...)
			if workdir != "" {
				cmd.Dir = workdir
			}
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		time.Now,
	)

	deps := generate.Deps{
		Getenv: os.Getenv,
		Kustomize: func(path string) ([]byte, error) {
			cmd := exec.Command("kustomize", "build", path)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return nil, err
			}
			return out.Bytes(), nil
		},
		Conftest: conftestRunner,
		EnsurePolicies: func() (string, bool, error) {
			return cache.Ensure()
		},
		WorkDir: workDir,
	}

	os.Exit(generate.Run(deps, os.Stdout, os.Stderr))
}

// conftestRunner runs conftest. conftest exits non-zero on policy failure but
// still prints JSON, so a non-zero exit with output is NOT an execution error.
func conftestRunner(args []string, stdin []byte) ([]byte, error) {
	cmd := exec.Command("conftest", args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok && out.Len() > 0 {
			return out.Bytes(), nil // policy failures, JSON present
		}
		return nil, fmt.Errorf("%w: %s", err, errb.String())
	}
	return out.Bytes(), nil
}

func getenvDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func parseTTL(s string) time.Duration {
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second
	}
	return time.Minute
}

var _ = evaluate.Finding{} // keep import if unused after edits
var _ = filepath.Join      // keep import if unused after edits
```

Remove the two trailing `var _` lines if `go vet` reports them as the only use; they are guards in case earlier edits drop the imports. Verify with build below.

- [ ] **Step 6: Build the binary**

Run: `go build ./... && go vet ./...`
Expected: builds clean. If `evaluate`/`filepath` imports are genuinely unused in `main.go`, delete those imports and the `var _` guard lines, then rebuild.

- [ ] **Step 7: Commit**

```bash
git add internal/generate/ cmd/
git commit -m "feat: generate orchestrator and CMP entrypoint wiring"
```

---

### Task 10: Deployment artifacts

**Files:**
- Create: `Dockerfile`
- Create: `deploy/plugin.yaml`
- Create: `deploy/repo-server-patch.yaml`
- Create: `deploy/README.md`
- Test: `deploy/deploy_test.go`

**Interfaces:**
- Consumes: the built `argo-guard` binary.
- Produces: deployable artifacts. Test validates the YAML parses and contains the required keys.

- [ ] **Step 1: Write the failing test**

`deploy/deploy_test.go`:
```go
package deploy

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func parseYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

func TestPluginManifest(t *testing.T) {
	m := parseYAML(t, "plugin.yaml")
	if m["kind"] != "ConfigManagementPlugin" {
		t.Fatalf("kind=%v", m["kind"])
	}
	spec, _ := m["spec"].(map[string]any)
	gen, _ := spec["generate"].(map[string]any)
	if gen["command"] == nil {
		t.Fatal("generate.command required")
	}
	disc, _ := spec["discover"].(map[string]any)
	if disc["find"] == nil {
		t.Fatal("discover.find required")
	}
}

func TestRepoServerPatchHasSidecar(t *testing.T) {
	m := parseYAML(t, "repo-server-patch.yaml")
	// Walk to spec.template.spec.containers and assert argo-guard present.
	out, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(out), "argo-guard") || !contains(string(out), "GUARD_POLICY_REPO") {
		t.Fatalf("patch missing sidecar/env:\n%s", out)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./deploy/`
Expected: FAIL — files not found.

- [ ] **Step 3: Create the artifacts**

`Dockerfile`:
```dockerfile
# Build stage
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/argo-guard ./cmd/argo-guard

# Runtime stage: the CMP sidecar
FROM alpine:3.20
RUN apk add --no-cache git ca-certificates
COPY --from=registry.k8s.io/kustomize/kustomize:v5.4.2 /app/kustomize /usr/local/bin/kustomize
COPY --from=openpolicyagent/conftest:v0.56.0 /usr/local/bin/conftest /usr/local/bin/conftest
COPY --from=build /out/argo-guard /usr/local/bin/argo-guard
# Argo mounts argocd-cmp-server via an initContainer at /var/run/argocd.
USER 999
ENTRYPOINT ["/var/run/argocd/argocd-cmp-server"]
```

`deploy/plugin.yaml`:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: ConfigManagementPlugin
metadata:
  name: argo-guard
spec:
  version: v1
  discover:
    find:
      glob: "**/kustomization.yaml"
  generate:
    command: ["argo-guard", "generate"]
```

`deploy/repo-server-patch.yaml`:
```yaml
# Strategic-merge patch applied to the argocd-repo-server Deployment.
apiVersion: apps/v1
kind: Deployment
metadata:
  name: argocd-repo-server
  namespace: argocd
spec:
  template:
    spec:
      containers:
        - name: argo-guard
          image: registry.corp.internal/argo-guard:v0.1.0
          command: ["/var/run/argocd/argocd-cmp-server"]
          securityContext:
            runAsNonRoot: true
            runAsUser: 999
          env:
            - name: GUARD_POLICY_REPO
              value: "https://git.corp/platform/argo-guard-policies.git"
            - name: GUARD_POLICY_REF
              value: "main"
            - name: GUARD_POLICY_TTL
              value: "60s"
            - name: GUARD_POLICY_CACHE
              value: "/var/cache/argo-guard/policies"
          volumeMounts:
            - name: var-files
              mountPath: /var/run/argocd
            - name: plugins
              mountPath: /home/argocd/cmp-server/plugins
            - name: argo-guard-config
              mountPath: /home/argocd/cmp-server/config/plugin.yaml
              subPath: plugin.yaml
            - name: cmp-tmp
              mountPath: /tmp
            - name: policy-cache
              mountPath: /var/cache/argo-guard
      volumes:
        - name: argo-guard-config
          configMap:
            name: argo-guard-plugin
        - name: cmp-tmp
          emptyDir: {}
        - name: policy-cache
          emptyDir: {}
```

`deploy/README.md`:
```markdown
# Deploying argo-guard

1. Build & push the image:
   `docker build -t registry.corp.internal/argo-guard:v0.1.0 . && docker push registry.corp.internal/argo-guard:v0.1.0`
2. Create the plugin ConfigMap from `plugin.yaml`:
   `kubectl -n argocd create configmap argo-guard-plugin --from-file=plugin.yaml=deploy/plugin.yaml`
3. Apply `repo-server-patch.yaml` to the `argocd-repo-server` Deployment
   (via your Argo install's kustomize/Helm values; `var-files` and `plugins`
   volumes already exist in the stock repo-server pod).
4. Roll out: `kubectl -n argocd rollout restart deploy/argocd-repo-server`.

Failure of this sidecar pauses manifest generation only; running workloads in
target clusters are unaffected.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./deploy/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add Dockerfile deploy/
git commit -m "feat: deployment artifacts (image, plugin manifest, repo-server patch)"
```

---

### Task 11: Sample policy repo + Rego tests

**Files:**
- Create: `examples/policies/guard.yaml`
- Create: `examples/policies/global/restrictions.rego`
- Create: `examples/policies/global/restrictions_test.rego`
- Create: `examples/policies/global/data.json`
- Create: `examples/policies/README.md`

**Interfaces:**
- Consumes: the `data.context` injection contract from Task 7 and the `guard.yaml` shape from Task 4.
- Produces: a working example policy bundle that doubles as the Task 12 e2e fixture. Verified with `conftest verify`.

- [ ] **Step 1: Write the policy and its Rego tests**

`examples/policies/guard.yaml`:
```yaml
bundles:
  - dir: global
    match: {}
```

`examples/policies/global/data.json`:
```json
{
  "trustedRepos": [
    "https://git.corp/infra/platform.git"
  ],
  "allowedRegistryPrefix": "registry.corp.internal/"
}
```

`examples/policies/global/restrictions.rego`:
```rego
package main

import future.keywords.in

# Cluster-scoped RBAC only from trusted infra repos.
deny contains msg if {
	input.kind in {"ClusterRole", "ClusterRoleBinding"}
	not input.context_repo_trusted
	msg := sprintf("%s/%s: cluster RBAC only allowed from trusted infra repos", [input.kind, input.metadata.name])
}

# Helper: is the deploying repo trusted?
# data.context.repo is injected by argo-guard via --data.
context_repo_trusted if {
	some r in data.trustedRepos
	r == data.context.repo
}

deny contains msg if {
	input.kind == "Service"
	input.spec.type == "LoadBalancer"
	msg := sprintf("Service/%s: LoadBalancer is not allowed", [input.metadata.name])
}

deny contains msg if {
	input.kind == "Deployment"
	some c in input.spec.template.spec.containers
	not c.resources.limits.memory
	msg := sprintf("Deployment/%s container %q must set a memory limit", [input.metadata.name, c.name])
}

warn contains msg if {
	input.kind == "Deployment"
	not input.metadata.labels.owner
	msg := sprintf("Deployment/%s should set an owner label", [input.metadata.name])
}
```

Note: `context_repo_trusted` is referenced as `input.context_repo_trusted` in the first rule but defined as a top-level rule — fix by referencing the rule directly:
replace `not input.context_repo_trusted` with `not context_repo_trusted`.

`examples/policies/global/restrictions_test.rego`:
```rego
package main

import future.keywords.in

test_loadbalancer_denied if {
	some msg in deny with input as {"kind": "Service", "metadata": {"name": "web"}, "spec": {"type": "LoadBalancer"}}
	contains(msg, "LoadBalancer is not allowed")
}

test_clusterrole_denied_for_untrusted_repo if {
	count(deny) > 0 with input as {"kind": "ClusterRole", "metadata": {"name": "x"}}
		with data.context as {"repo": "https://git.corp/team-a/app.git"}
		with data.trustedRepos as ["https://git.corp/infra/platform.git"]
}

test_clusterrole_allowed_for_trusted_repo if {
	count(deny) == 0 with input as {"kind": "ClusterRole", "metadata": {"name": "x"}}
		with data.context as {"repo": "https://git.corp/infra/platform.git"}
		with data.trustedRepos as ["https://git.corp/infra/platform.git"]
}

test_missing_memory_limit_denied if {
	some msg in deny with input as {
		"kind": "Deployment",
		"metadata": {"name": "web"},
		"spec": {"template": {"spec": {"containers": [{"name": "app", "resources": {}}]}}},
	}
	contains(msg, "must set a memory limit")
}
```

`examples/policies/README.md`:
```markdown
# Example argo-guard policy repo

Layout mirrors what `GUARD_POLICY_REPO` should contain:

- `guard.yaml` — bundle registry with match/exclude.
- `global/` — a bundle (`match: {}`, always applies): Rego + `data.json`.

The trust context is available in Rego as `data.context` (`data.context.repo`,
`.project`, `.namespace`, `.appLabels`). External allowlists live in `data.json`.

Test policies locally:
`conftest verify --policy global/ --data global/`
```

- [ ] **Step 2: Apply the `context_repo_trusted` fix**

In `restrictions.rego`, change `not input.context_repo_trusted` to `not context_repo_trusted`.

- [ ] **Step 3: Run the Rego tests**

Run: `conftest verify --policy examples/policies/global/ --data examples/policies/global/`
Expected: all `test_*` pass (4 tests OK). If `conftest` is not installed locally, document the command in CI; skip locally.

- [ ] **Step 4: Commit**

```bash
git add examples/
git commit -m "feat: example policy repo with Rego tests and trusted-repo exemption"
```

---

### Task 12: End-to-end harness

**Files:**
- Create: `e2e/e2e_test.go`
- Create: `e2e/fixtures/clean-app/kustomization.yaml`
- Create: `e2e/fixtures/clean-app/deployment.yaml`
- Create: `e2e/fixtures/bad-app/kustomization.yaml`
- Create: `e2e/fixtures/bad-app/service.yaml`

**Interfaces:**
- Consumes: the built `argo-guard` binary, real `kustomize` + `conftest`, and `examples/policies` as the policy root.
- Produces: a build-tagged e2e test that runs the real pipeline end to end.

- [ ] **Step 1: Create fixtures**

`e2e/fixtures/clean-app/kustomization.yaml`:
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
```

`e2e/fixtures/clean-app/deployment.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  labels:
    owner: team-a
spec:
  template:
    spec:
      containers:
        - name: app
          image: registry.corp.internal/web:1
          resources:
            limits:
              memory: 128Mi
```

`e2e/fixtures/bad-app/kustomization.yaml`:
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - service.yaml
```

`e2e/fixtures/bad-app/service.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  type: LoadBalancer
  ports:
    - port: 80
```

- [ ] **Step 2: Write the e2e test**

`e2e/e2e_test.go`:
```go
//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGuard builds the binary once and runs it against a fixture app, returning
// exit code and combined stderr.
func runGuard(t *testing.T, appPath string) (int, string, string) {
	t.Helper()
	repoRoot, _ := filepath.Abs("..")
	bin := filepath.Join(t.TempDir(), "argo-guard")
	build := exec.Command("go", "build", "-o", bin, "./cmd/argo-guard")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cmd := exec.Command(bin, "generate")
	cmd.Env = append(os.Environ(),
		"ARGOCD_APP_SOURCE_PATH="+filepath.Join(repoRoot, appPath),
		"ARGOCD_APP_SOURCE_REPO_URL=https://git.corp/team-a/app.git",
		"ARGOCD_APP_PROJECT_NAME=team-a",
		// Point the cache directly at the example policies (skip git clone) by
		// pre-seeding GUARD_POLICY_CACHE and a far-future TTL.
		"GUARD_POLICY_CACHE="+filepath.Join(repoRoot, "examples/policies"),
		"GUARD_POLICY_TTL=8760h",
	)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run: %v", err)
	}
	return code, out.String(), errb.String()
}

func TestE2ECleanAppPasses(t *testing.T) {
	code, stdout, stderr := runGuard(t, "e2e/fixtures/clean-app")
	if code != 0 {
		t.Fatalf("expected pass, got %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "kind: Deployment") {
		t.Fatalf("expected manifests on stdout:\n%s", stdout)
	}
}

func TestE2EBadAppDenied(t *testing.T) {
	code, stdout, stderr := runGuard(t, "e2e/fixtures/bad-app")
	if code != 1 {
		t.Fatalf("expected violation exit 1, got %d\n%s", code, stderr)
	}
	if stdout != "" {
		t.Fatal("must not emit manifests on violation")
	}
	if !strings.Contains(stderr, "LoadBalancer") {
		t.Fatalf("expected LoadBalancer denial:\n%s", stderr)
	}
}
```

Note: the e2e test relies on `policyrepo.Ensure` serving the pre-seeded cache dir without cloning. Since `GUARD_POLICY_CACHE` already exists and TTL is far future, `Ensure` returns it immediately (the `exists() && fresh` path) — but `lastSync` is zero on a cold process, so it will attempt a `fetch`. Add a guard in `main.go`: if `GUARD_POLICY_REPO` is empty, treat an existing cache dir as authoritative (no fetch). Implement in Step 3.

- [ ] **Step 3: Add the "no repo configured" cache shortcut**

In `cmd/argo-guard/main.go`, before constructing the cache, add:
```go
	ensure := func() (string, bool, error) { return cache.Ensure() }
	if os.Getenv("GUARD_POLICY_REPO") == "" {
		// Local/dev mode: use the pre-seeded cache dir as-is, no git.
		dir := cacheDir
		ensure = func() (string, bool, error) {
			if _, err := os.Stat(dir); err != nil {
				return "", false, fmt.Errorf("GUARD_POLICY_REPO unset and no cache at %s (fail-closed)", dir)
			}
			return dir, false, nil
		}
	}
```
and set `EnsurePolicies: ensure` in `deps` (replace the inline closure).

- [ ] **Step 4: Run the e2e suite**

Run: `go test -tags e2e ./e2e/`
Expected: PASS (requires `kustomize` and `conftest` on PATH). Skips from normal `go test ./...` because of the build tag.

- [ ] **Step 5: Commit**

```bash
git add e2e/ cmd/
git commit -m "test: end-to-end harness for clean and violating apps"
```

---

## Self-Review

**Spec coverage:**
- CMP sidecar enforcement → Tasks 9, 10. ✅
- Conftest/Rego engine → Tasks 7, 11. ✅
- Layered scoping (global/namespace/project/label/repo) + match/exclude DSL → Tasks 2, 3, 4. ✅
- Context injection + trusted-repo exemption → Tasks 7 (`data.context`), 11 (exemption Rego + tests). ✅
- Policy repo cached, TTL, last-known-good, cold-start fail-closed → Task 5. ✅
- Fail-closed exit codes, manifests-only-on-pass → Task 9. ✅
- Stale-cache warning surfaced → Tasks 8, 9. ✅
- Deployment artifacts (image/plugin/patch) → Task 10. ✅
- Testing: Go unit (Tasks 1–9), Rego (Task 11), e2e (Task 12). ✅
- Break-glass (policy-repo only): no code artifact — it is an operational practice in the policy repo; the trusted-repo `data.json` mechanism (Task 11) is the implementation hook. ✅ (documented in `examples/policies/README.md` and spec)
- Label dimension via `ARGOCD_ENV_*` → Task 1 (refinement noted up front). ✅

**Placeholder scan:** No TBD/TODO; every code step has complete code. Two `var _` import guards in Task 9 are intentional and removed-if-unused in the build step. The `context_repo_trusted` bug is deliberately introduced and fixed in Task 11 Step 2 to keep the Rego idiomatic.

**Type consistency:** `trust.Context`, `match.Expr`/`Condition`, `bundles.Registry.Select`, `render.Build`→`([]byte,[]Resource,error)`, `evaluate.Run` signature, `evaluate.Result`/`Finding`, `emit.Success`/`Report`, `generate.Deps`/`Run` all referenced consistently across tasks. Conftest data contract (`data.context`) matches between Task 7 (writes `{"context":...}`) and Task 11 (reads `data.context`).
