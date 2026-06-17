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
