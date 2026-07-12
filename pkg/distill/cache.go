// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bborbe/errors"
)

//counterfeiter:generate -o ../../mocks/distill-cache.go --fake-name DistillCache . Cache

// Cache stores validated bullets keyed by rule id, guarded by a content hash so
// a changed rule (or changed compression context) is a miss.
type Cache interface {
	// Get returns the cached bullet for id when the stored hash equals hash.
	Get(id, hash string) (bullet string, ok bool)
	// Put records a validated bullet for id under hash (pending write).
	Put(id, hash, bullet string)
	// Save persists the cache, pruning to exactly keepIDs on a successful run.
	Save(ctx context.Context, keepIDs []string) error
	// SaveMerged persists all currently-Put bullets without pruning any ids.
	SaveMerged(ctx context.Context) error
	// Load reads the cache from disk; a missing/corrupt file warns and runs cold.
	Load(ctx context.Context) error
	// RuleHash derives the content hash for a rule body under the current
	// compression context (prompt-version constant + system.md + model + body).
	RuleHash(model, body string) string
}

// cachePromptVersion bumps whenever the compression contract changes in a way
// that must invalidate every cache entry. Increment on any system.md or output-
// contract change.
const cachePromptVersion = "v3"

// cacheFile is the JSON schema persisted to disk.
type cacheFile struct {
	Version string                `json:"version"`
	Entries map[string]cacheEntry `json:"entries"`
}

// cacheEntry holds one cached bullet with its content hash.
type cacheEntry struct {
	Hash   string `json:"hash"`
	Bullet string `json:"bullet"`
}

// RuleHash derives the content hash for a rule body under the current
// compression context (prompt-version constant + system.md + model + body).
// Each component is length-prefixed so distinct inputs cannot collide via concatenation.
func RuleHash(model, body string) string {
	h := sha256.New()
	for _, s := range []string{cachePromptVersion, SystemPrompt(), model, body} {
		fmt.Fprintf(h, "%d\n", len(s))
		_, _ = io.WriteString(h, s)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// NewFileCache returns a file-backed Cache that reads/writes a JSON cache at path.
// Warning messages (missing, corrupt, version-mismatch) are written to warn.
func NewFileCache(path string, warn io.Writer) Cache {
	return &fileCache{
		path:    path,
		warn:    warn,
		loaded:  map[string]cacheEntry{},
		pending: map[string]cacheEntry{},
	}
}

// fileCache is a file-backed Cache. Driver is single-goroutine, so the pending
// map is accessed without a mutex.
type fileCache struct {
	path    string
	warn    io.Writer
	loaded  map[string]cacheEntry
	pending map[string]cacheEntry
}

// RuleHash delegates to the package-level function.
func (c *fileCache) RuleHash(model, body string) string {
	return RuleHash(model, body)
}

// Load reads the cache from disk and populates the in-memory loaded map.
// A missing, unparseable, or version-mismatched file warns to warn and runs cold.
// Load never returns a non-nil error; all failure modes result in a cold run.
func (c *fileCache) Load(_ context.Context) error {
	// #nosec G304 -- path is constructed by pkg/cli from the sourceDir argument, not from user-supplied free text
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(c.warn, "distill: cache file %q missing — running cold\n", c.path)
		} else {
			fmt.Fprintf(c.warn, "distill: cache file %q corrupt — ignoring, running cold\n", c.path)
		}
		return nil
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		fmt.Fprintf(c.warn, "distill: cache file %q corrupt — ignoring, running cold\n", c.path)
		return nil
	}
	if cf.Version != cachePromptVersion {
		fmt.Fprintf(c.warn, "distill: cache schema version mismatch — running cold\n")
		return nil
	}
	c.loaded = cf.Entries
	if c.loaded == nil {
		c.loaded = map[string]cacheEntry{}
	}
	return nil
}

// Get returns the cached bullet for id when the stored hash equals hash.
func (c *fileCache) Get(id, hash string) (string, bool) {
	e, ok := c.loaded[id]
	if !ok || e.Hash != hash {
		return "", false
	}
	return e.Bullet, true
}

// Put records a validated bullet for id under hash into the pending map.
// Driver is single-goroutine, so no mutex is used.
func (c *fileCache) Put(id, hash, bullet string) {
	c.pending[id] = cacheEntry{Hash: hash, Bullet: bullet}
}

// Save persists the cache pruned to exactly keepIDs, dropping removed/disabled ids.
func (c *fileCache) Save(ctx context.Context, keepIDs []string) error {
	keep := make(map[string]struct{}, len(keepIDs))
	for _, id := range keepIDs {
		keep[id] = struct{}{}
	}
	entries := make(map[string]cacheEntry)
	for id, e := range c.loaded {
		if _, ok := keep[id]; ok {
			entries[id] = e
		}
	}
	for id, e := range c.pending {
		if _, ok := keep[id]; ok {
			entries[id] = e
		}
	}
	return c.persist(ctx, entries)
}

// SaveMerged persists all entries (loaded + pending) without pruning any ids.
func (c *fileCache) SaveMerged(ctx context.Context) error {
	entries := make(map[string]cacheEntry, len(c.loaded)+len(c.pending))
	for id, e := range c.loaded {
		entries[id] = e
	}
	for id, e := range c.pending {
		entries[id] = e
	}
	return c.persist(ctx, entries)
}

// persist atomically writes entries to disk via temp-file + rename, mirroring writer.go.
func (c *fileCache) persist(ctx context.Context, entries map[string]cacheEntry) error {
	cf := cacheFile{
		Version: cachePromptVersion,
		Entries: entries,
	}
	data, err := json.Marshal(cf)
	if err != nil {
		return errors.Wrapf(ctx, err, "marshal cache")
	}
	dir := filepath.Dir(c.path)
	tmp, err := os.CreateTemp(dir, ".distill-cache-*.tmp")
	if err != nil {
		return errors.Wrapf(ctx, err, "create cache temp file in %q", dir)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return errors.Wrapf(ctx, err, "write cache temp file %q", tmpName)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return errors.Wrapf(ctx, err, "fsync cache temp file %q", tmpName)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return errors.Wrapf(ctx, err, "close cache temp file %q", tmpName)
	}
	if err := os.Rename(tmpName, c.path); err != nil {
		cleanup()
		return errors.Wrapf(ctx, err, "rename cache %q onto %q", tmpName, c.path)
	}
	return nil
}

// NewNoopCache returns a Cache that never stores or retrieves anything.
// Used when --no-cache is passed; RuleHash still works so the Driver can
// compute hashes for logging even though nothing is persisted.
func NewNoopCache() Cache {
	return &noopCache{}
}

type noopCache struct{}

func (c *noopCache) RuleHash(model, body string) string { return RuleHash(model, body) }

func (c *noopCache) Get(_, _ string) (string, bool) { return "", false }

func (c *noopCache) Put(_, _, _ string) {}

func (c *noopCache) Load(_ context.Context) error { return nil }

func (c *noopCache) Save(_ context.Context, _ []string) error { return nil }

func (c *noopCache) SaveMerged(_ context.Context) error { return nil }
