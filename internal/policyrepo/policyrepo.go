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
