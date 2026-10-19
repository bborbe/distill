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

// CacheStatus classifies how a loaded cache relates to the output file of the
// current run.
type CacheStatus int

const (
	// CacheCold means there is nothing to serve: the cache file is missing or
	// corrupt, or the output file needed for a rebuild is missing.
	CacheCold CacheStatus = iota
	// CacheWarm means the cache is bound to this run's output and its recorded
	// content hash matches the file on disk.
	CacheWarm
	// CacheRebuild means the cache exists but is unbound or its recorded output
	// hash is stale; entries must be rebuilt from the output file.
	CacheRebuild
	// CacheForeign means the cache belongs to a different output path.
	CacheForeign
)

// Cache stores validated bullets keyed by rule id, guarded by a content hash so
// a changed rule (or changed compression context) is a miss.
type Cache interface {
	// Get returns the cached bullet for id when the stored hash equals hash.
	Get(id, hash string) (bullet string, ok bool)
	// GetPrevious returns the last validated bullet for id regardless of its
	// content hash, so a changed rule can be revised from its prior rendering
	// instead of re-derived from scratch.
	GetPrevious(id string) (bullet string, ok bool)
	// Put records a validated bullet for id under hash (pending write).
	Put(id, hash, bullet string)
	// Load reads the cache from disk and classifies it against outputPath,
	// which must be the normalized absolute form of the run's --output.
	Load(ctx context.Context, outputPath string) error
	// Status reports the classification produced by the last Load.
	Status() CacheStatus
	// Save persists the cache pruned to keepIDs and binds it to outputPath and
	// the SHA-256 of the bytes currently at outputPath.
	Save(ctx context.Context, outputPath string, keepIDs []string) error
	// SaveMerged persists all entries without pruning and without changing the
	// binding recorded by Load.
	SaveMerged(ctx context.Context) error
	// RuleHash derives the content hash for a rule body under the current
	// compression context (prompt-version constant + system.md + model + body).
	RuleHash(model, body string) string
}

// cachePromptVersion bumps whenever the compression contract changes in a way
// that must invalidate every cache entry. Increment on any system.md or output-
// contract change.
//
// A schema-only change must NOT bump this constant: a cache without the output
// binding fields is unbound and is rebuilt from the committed output. A future
// compression-contract bump must split the schema and compression constants
// (or force a cold compile) before bumping, otherwise the rebuild path would
// adopt old-contract bullets as current and skip the recompile the bump exists
// to force.
const cachePromptVersion = "v4"

// cacheFile is the JSON schema persisted to disk.
type cacheFile struct {
	Version    string                `json:"version"`
	OutputPath string                `json:"output_path,omitempty"`
	OutputHash string                `json:"output_hash,omitempty"`
	Entries    map[string]cacheEntry `json:"entries"`
}

// cacheEntry holds one cached bullet with its content hash.
type cacheEntry struct {
	Hash   string `json:"hash"`
	Bullet string `json:"bullet"`
}

// cacheBinding is the output binding recorded by Load and reused by SaveMerged.
type cacheBinding struct {
	OutputPath string
	OutputHash string
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

// normalizeOutputPath returns the absolute, cleaned form of path. Symlinks are
// not resolved, so two spellings of the same path share one binding.
func normalizeOutputPath(ctx context.Context, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "resolve absolute output path %q", path)
	}
	return filepath.Clean(abs), nil
}

// hashOutput returns the hex SHA-256 of content.
func hashOutput(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
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
	status  CacheStatus
	binding cacheBinding
}

// RuleHash delegates to the package-level function.
func (c *fileCache) RuleHash(model, body string) string {
	return RuleHash(model, body)
}

// Load reads the cache from disk and classifies it against outputPath.
// A missing, unreadable, corrupt, unbound, or foreign cache leaves the loaded
// map empty so Get cannot serve stale entries. Load never returns a non-nil
// error; every failure mode is a warning plus a cold, rebuild, or foreign status.
func (c *fileCache) Load(_ context.Context, outputPath string) error {
	c.reset()
	// #nosec G304 -- path is constructed by pkg/cli from the sourceDir argument, not from user-supplied free text
	data, err := os.ReadFile(c.path)
	if err != nil {
		c.classifyReadError(err)
		return nil
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		c.warnf("distill: cache file %q corrupt — ignoring, running cold\n", c.path)
		return nil
	}
	c.classify(cf, outputPath)
	return nil
}

// reset clears all state so a repeated Load cannot retain entries or a binding.
func (c *fileCache) reset() {
	c.loaded = map[string]cacheEntry{}
	c.pending = map[string]cacheEntry{}
	c.status = CacheCold
	c.binding = cacheBinding{}
}

// classifyReadError maps a cache-file read failure to a cold status and warning.
func (c *fileCache) classifyReadError(err error) {
	if os.IsNotExist(err) {
		c.warnf("distill: cache file %q missing — running cold\n", c.path)
		return
	}
	c.warnf("distill: cache file %q unreadable — running cold\n", c.path)
}

// classify applies the schema, binding, and content-hash checks in order.
func (c *fileCache) classify(cf cacheFile, outputPath string) {
	if cf.Version != cachePromptVersion {
		c.classifyMissingBinding(outputPath, "cache schema version mismatch")
		return
	}
	if cf.OutputPath == "" || cf.OutputHash == "" {
		c.classifyMissingBinding(outputPath, "cache has no output binding")
		return
	}
	c.classifyBound(cf, outputPath)
}

// classifyMissingBinding handles caches that cannot describe this run's output:
// rebuild when the output exists, cold when it does not.
func (c *fileCache) classifyMissingBinding(outputPath, reason string) {
	if _, err := os.Stat(outputPath); err == nil {
		c.status = CacheRebuild
		c.warnf("distill: %s — rebuilding from output\n", reason)
		return
	}
	c.warnf("distill: %s — running cold\n", reason)
}

// classifyBound checks the recorded path and content hash against this run.
func (c *fileCache) classifyBound(cf cacheFile, outputPath string) {
	c.binding = cacheBinding{OutputPath: cf.OutputPath, OutputHash: cf.OutputHash}
	if cf.OutputPath != outputPath {
		c.status = CacheForeign
		c.warnf(
			"distill: cache file %q belongs to output %q, not %q — "+
				"run against that output or delete the cache file\n",
			c.path,
			cf.OutputPath,
			outputPath,
		)
		return
	}
	// #nosec G304 -- outputPath is the operator-supplied --output argument, not free-form input
	data, err := os.ReadFile(outputPath)
	if err != nil {
		c.warnf("distill: output file %q missing — running cold\n", outputPath)
		return
	}
	if hashOutput(data) != cf.OutputHash {
		c.status = CacheRebuild
		c.warnf("distill: cache output hash mismatch for %q — rebuilding from output\n", outputPath)
		return
	}
	c.status = CacheWarm
	c.loaded = cf.Entries
	if c.loaded == nil {
		c.loaded = map[string]cacheEntry{}
	}
}

// warnf writes a cache warning to the configured writer.
func (c *fileCache) warnf(format string, args ...any) {
	fmt.Fprintf(c.warn, format, args...)
}

// Status reports the classification produced by the last Load.
func (c *fileCache) Status() CacheStatus {
	return c.status
}

// Get returns the cached bullet for id when the stored hash equals hash.
func (c *fileCache) Get(id, hash string) (string, bool) {
	e, ok := c.loaded[id]
	if !ok || e.Hash != hash {
		return "", false
	}
	return e.Bullet, true
}

// GetPrevious returns the last validated bullet for id regardless of its hash.
// It reads only the loaded map: a bullet recorded during this run is already in
// bulletByID and is never a revision source for another rule.
func (c *fileCache) GetPrevious(id string) (string, bool) {
	e, ok := c.loaded[id]
	if !ok {
		return "", false
	}
	return e.Bullet, true
}

// Put records a validated bullet for id under hash into the pending map.
// Driver is single-goroutine, so no mutex is used.
func (c *fileCache) Put(id, hash, bullet string) {
	c.pending[id] = cacheEntry{Hash: hash, Bullet: bullet}
}

// Save persists the cache pruned to exactly keepIDs and binds it to outputPath
// and the SHA-256 of the bytes currently at outputPath.
func (c *fileCache) Save(ctx context.Context, outputPath string, keepIDs []string) error {
	// #nosec G304 -- outputPath is the operator-supplied --output argument, not free-form input
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return errors.Wrapf(ctx, err, "read output %q for cache binding", outputPath)
	}
	return c.persist(ctx, cacheFile{
		Version:    cachePromptVersion,
		OutputPath: outputPath,
		OutputHash: hashOutput(data),
		Entries:    c.prunedEntries(keepIDs),
	})
}

// prunedEntries returns the union of loaded and pending entries, filtered to
// keepIDs (pending wins).
func (c *fileCache) prunedEntries(keepIDs []string) map[string]cacheEntry {
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
	return entries
}

// SaveMerged persists all entries (loaded + pending) without pruning any ids.
// It preserves the binding recorded by Load and never rebinds.
func (c *fileCache) SaveMerged(ctx context.Context) error {
	return c.persist(ctx, cacheFile{
		Version:    cachePromptVersion,
		OutputPath: c.binding.OutputPath,
		OutputHash: c.binding.OutputHash,
		Entries:    c.allEntries(),
	})
}

// allEntries returns the union of loaded and pending entries (pending wins).
func (c *fileCache) allEntries() map[string]cacheEntry {
	entries := make(map[string]cacheEntry, len(c.loaded)+len(c.pending))
	for id, e := range c.loaded {
		entries[id] = e
	}
	for id, e := range c.pending {
		entries[id] = e
	}
	return entries
}

// persist atomically writes cf to disk via temp-file + rename, mirroring writer.go.
func (c *fileCache) persist(ctx context.Context, cf cacheFile) error {
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

func (c *noopCache) GetPrevious(_ string) (string, bool) { return "", false }

func (c *noopCache) Put(_, _, _ string) {}

func (c *noopCache) Load(_ context.Context, _ string) error { return nil }

func (c *noopCache) Status() CacheStatus { return CacheCold }

func (c *noopCache) Save(_ context.Context, _ string, _ []string) error { return nil }

func (c *noopCache) SaveMerged(_ context.Context) error { return nil }
