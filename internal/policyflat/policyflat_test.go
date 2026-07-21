package policyflat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModeParsing(t *testing.T) {
	cases := []struct {
		in      string
		want    bool
		wantErr bool
	}{
		{"", false, false},
		{"true", true, false},
		{"1", false, true},
		{"false", false, true},
		{"TRUE", false, true},
		{"yes", false, true},
	}
	for _, c := range cases {
		got, err := Mode(c.in)
		if c.wantErr && err == nil {
			t.Errorf("Mode(%q): want error, got none", c.in)
		}
		if !c.wantErr && err != nil {
			t.Errorf("Mode(%q): unexpected error %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("Mode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// writeFlat populates dir with plain files keyed by flattened names.
func writeFlat(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExpandBuildsTree(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFlat(t, src, map[string]string{
		"guard.yaml":                "bundles:\n  - dir: global\n    match: {}\n",
		"global__restrictions.rego": "package main\n",
		"global__data.json":         `{"trustedRepos":[]}`,
	})
	if err := Expand(src, dst); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{
		"guard.yaml":               "bundles:\n  - dir: global\n    match: {}\n",
		"global/restrictions.rego": "package main\n",
		"global/data.json":         `{"trustedRepos":[]}`,
	} {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("%s: content %q, want %q", rel, got, want)
		}
	}
}

func TestExpandMultiLevel(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFlat(t, src, map[string]string{"namespaces__payments__rules.rego": "package main\n"})
	if err := Expand(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "namespaces", "payments", "rules.rego")); err != nil {
		t.Fatal(err)
	}
}

func TestExpandSkipsDotEntries(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFlat(t, src, map[string]string{"guard.yaml": "x", ".hidden": "x"})
	if err := os.MkdirAll(filepath.Join(src, "..2026_07_21_00_00_00.0000"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Expand(src, dst); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "guard.yaml" {
		t.Fatalf("want only guard.yaml in dst, got %v", entries)
	}
}

// TestExpandFollowsSymlinkedFiles reproduces the exact shape kubelet gives a
// ConfigMap volume: a timestamped data dir, a ..data symlink to it, and a
// top-level relative symlink per key.
func TestExpandFollowsSymlinkedFiles(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	dataDir := "..2026_07_21_10_00_00.1234567890"
	if err := os.MkdirAll(filepath.Join(src, dataDir), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFlat(t, filepath.Join(src, dataDir), map[string]string{
		"guard.yaml":                "bundles: []\n",
		"global__restrictions.rego": "package main\n",
	})
	if err := os.Symlink(dataDir, filepath.Join(src, "..data")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"guard.yaml", "global__restrictions.rego"} {
		if err := os.Symlink(filepath.Join("..data", name), filepath.Join(src, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := Expand(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "global", "restrictions.rego"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestExpandRejectsSubdirectory(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "realdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Expand(src, dst); err == nil || !strings.Contains(err.Error(), "realdir") {
		t.Fatalf("want directory rejection naming realdir, got %v", err)
	}
}

func TestExpandRejectsBadSegments(t *testing.T) {
	for _, name := range []string{"__lead.rego", "a____b.rego", "trail__", "a__..__b.rego"} {
		src, dst := t.TempDir(), t.TempDir()
		writeFlat(t, src, map[string]string{name: "x"})
		if err := Expand(src, dst); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("key %q: want rejection naming the key, got %v", name, err)
		}
	}
}

func TestExpandFileDirCollision(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFlat(t, src, map[string]string{"a__b": "x", "a__b__c": "y"})
	if err := Expand(src, dst); err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("want collision error, got %v", err)
	}
}

func TestExpandEmptyDirFailsClosed(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFlat(t, src, map[string]string{".only-dotfiles": "x"})
	if err := Expand(src, dst); err == nil || !strings.Contains(err.Error(), "fail-closed") {
		t.Fatalf("want fail-closed error for empty dir, got %v", err)
	}
}
