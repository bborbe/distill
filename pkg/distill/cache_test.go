// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/distill/pkg/distill"
)

var _ = Describe("RuleHash", func() {
	It("is stable for identical inputs", func() {
		h1 := distill.RuleHash("model", "body")
		h2 := distill.RuleHash("model", "body")
		Expect(h1).To(Equal(h2))
	})

	It("differs when model changes", func() {
		Expect(distill.RuleHash("model-a", "body")).
			NotTo(Equal(distill.RuleHash("model-b", "body")))
	})

	It("differs when body changes", func() {
		Expect(distill.RuleHash("model", "body-a")).
			NotTo(Equal(distill.RuleHash("model", "body-b")))
	})
})

// sha256Hex returns the hex SHA-256 of s, matching the cache's output hash.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

var _ = Describe("FileCache", func() {
	var (
		ctx        context.Context
		cacheDir   string
		cachePath  string
		outputPath string
		warnBuf    bytes.Buffer
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		cacheDir, err = os.MkdirTemp("", "distill-cache-unit-*")
		Expect(err).NotTo(HaveOccurred())
		cachePath = filepath.Join(cacheDir, "test-cache.json")
		outputPath = filepath.Join(cacheDir, "out.md")
		Expect(os.WriteFile(outputPath, []byte("output"), 0o600)).To(Succeed())
		warnBuf.Reset()
	})

	AfterEach(func() {
		_ = os.RemoveAll(cacheDir)
	})

	readCacheFile := func() []byte {
		data, err := os.ReadFile(cachePath)
		Expect(err).NotTo(HaveOccurred())
		return data
	}

	readCacheBinding := func() (string, string) {
		var cf struct {
			OutputPath string `json:"output_path"`
			OutputHash string `json:"output_hash"`
		}
		Expect(json.Unmarshal(readCacheFile(), &cf)).To(Succeed())
		return cf.OutputPath, cf.OutputHash
	}

	writeCacheFile := func(content string) {
		Expect(os.WriteFile(cachePath, []byte(content), 0o600)).To(Succeed())
	}

	It("Load on missing file warns and runs cold (Get always misses)", func() {
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheCold))
		Expect(warnBuf.String()).To(ContainSubstring("missing"))
		_, ok := c.Get("any", "hash")
		Expect(ok).To(BeFalse())
	})

	It("Load on corrupt JSON warns and runs cold", func() {
		writeCacheFile("{not json")
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheCold))
		Expect(warnBuf.String()).To(ContainSubstring("corrupt"))
		_, ok := c.Get("any", "hash")
		Expect(ok).To(BeFalse())
	})

	It("Load on an unreadable cache file warns and runs cold", func() {
		// A directory at the cache path makes os.ReadFile fail with a non-ENOENT error.
		Expect(os.Mkdir(cachePath, 0o750)).To(Succeed())
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheCold))
		Expect(warnBuf.String()).To(ContainSubstring("unreadable"))
		_, ok := c.Get("any", "hash")
		Expect(ok).To(BeFalse())
	})

	It("Load on version-mismatch rebuilds from the existing output", func() {
		data, err := json.Marshal(map[string]interface{}{
			"version": "v0",
			"entries": map[string]interface{}{
				"a": map[string]string{"hash": "h", "bullet": "- **A.** text"},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(cachePath, data, 0o600)).To(Succeed())

		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheRebuild))
		Expect(warnBuf.String()).To(ContainSubstring("mismatch"))
		Expect(warnBuf.String()).To(ContainSubstring("rebuild"))
		_, ok := c.Get("a", "h")
		Expect(ok).To(BeFalse())
	})

	It("Load on version-mismatch without an output runs cold", func() {
		Expect(os.Remove(outputPath)).To(Succeed())
		data, err := json.Marshal(map[string]interface{}{
			"version": "v0",
			"entries": map[string]interface{}{},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(cachePath, data, 0o600)).To(Succeed())

		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheCold))
		Expect(warnBuf.String()).To(ContainSubstring("mismatch"))
		Expect(warnBuf.String()).To(ContainSubstring("cold"))
	})

	It("Save records the output path and content hash", func() {
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		c.Put("a", "hash-a", "- **A.** bullet")
		Expect(c.Save(ctx, outputPath, []string{"a"})).To(Succeed())

		gotPath, gotHash := readCacheBinding()
		Expect(gotPath).To(Equal(outputPath))
		Expect(gotHash).To(Equal(sha256Hex("output")))

		c2 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c2.Load(ctx, outputPath)).To(Succeed())
		Expect(c2.Status()).To(Equal(distill.CacheWarm))
		bullet, ok := c2.Get("a", "hash-a")
		Expect(ok).To(BeTrue())
		Expect(bullet).To(Equal("- **A.** bullet"))
	})

	It("round-trip: Put + Save → Load → Get hits", func() {
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		c.Put("a", "hash-a", "- **A.** bullet a")
		c.Put("b", "hash-b", "- **B.** bullet b")
		Expect(c.Save(ctx, outputPath, []string{"a", "b"})).To(Succeed())

		c2 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c2.Load(ctx, outputPath)).To(Succeed())
		Expect(c2.Status()).To(Equal(distill.CacheWarm))

		bullet, ok := c2.Get("a", "hash-a")
		Expect(ok).To(BeTrue())
		Expect(bullet).To(Equal("- **A.** bullet a"))

		bullet, ok = c2.Get("b", "hash-b")
		Expect(ok).To(BeTrue())
		Expect(bullet).To(Equal("- **B.** bullet b"))

		// wrong hash → miss
		_, ok = c2.Get("a", "wrong-hash")
		Expect(ok).To(BeFalse())
	})

	It("prune: Save drops ids not in keepIDs", func() {
		// Seed cache with a and b.
		c1 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c1.Load(ctx, outputPath)).To(Succeed())
		c1.Put("a", "hash-a", "- **A.** bullet")
		c1.Put("b", "hash-b", "- **B.** bullet")
		Expect(c1.Save(ctx, outputPath, []string{"a", "b"})).To(Succeed())

		// Save with only "a" — prunes b.
		c2 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c2.Load(ctx, outputPath)).To(Succeed())
		Expect(c2.Save(ctx, outputPath, []string{"a"})).To(Succeed())

		// Reload and verify a present, b absent.
		c3 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c3.Load(ctx, outputPath)).To(Succeed())
		_, okA := c3.Get("a", "hash-a")
		Expect(okA).To(BeTrue())
		_, okB := c3.Get("b", "hash-b")
		Expect(okB).To(BeFalse())
	})

	It("SaveMerged: no pruning — loaded + Put all persist", func() {
		// Seed with a and b.
		c1 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c1.Load(ctx, outputPath)).To(Succeed())
		c1.Put("a", "hash-a", "- **A.** bullet")
		c1.Put("b", "hash-b", "- **B.** bullet")
		Expect(c1.Save(ctx, outputPath, []string{"a", "b"})).To(Succeed())

		// Load a,b; Put c; SaveMerged.
		c2 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c2.Load(ctx, outputPath)).To(Succeed())
		c2.Put("c", "hash-c", "- **C.** bullet")
		Expect(c2.SaveMerged(ctx)).To(Succeed())

		// Reload and verify all three present.
		c3 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c3.Load(ctx, outputPath)).To(Succeed())
		_, okA := c3.Get("a", "hash-a")
		Expect(okA).To(BeTrue())
		_, okB := c3.Get("b", "hash-b")
		Expect(okB).To(BeTrue())
		_, okC := c3.Get("c", "hash-c")
		Expect(okC).To(BeTrue())
	})

	It("foreign output: serves no entries, warns, and leaves the cache untouched", func() {
		outputA := filepath.Join(cacheDir, "a.md")
		outputB := filepath.Join(cacheDir, "b.md")
		Expect(os.WriteFile(outputA, []byte("A"), 0o600)).To(Succeed())
		Expect(os.WriteFile(outputB, []byte("B"), 0o600)).To(Succeed())

		seed := distill.NewFileCache(cachePath, &warnBuf)
		Expect(seed.Load(ctx, outputA)).To(Succeed())
		seed.Put("a", "hash-a", "- **A.** bullet")
		Expect(seed.Save(ctx, outputA, []string{"a"})).To(Succeed())
		before := readCacheFile()

		warnBuf.Reset()
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputB)).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheForeign))
		Expect(warnBuf.String()).To(ContainSubstring(outputA))
		Expect(warnBuf.String()).To(ContainSubstring("run against that output"))
		Expect(warnBuf.String()).To(ContainSubstring("delete the cache"))
		_, ok := c.Get("a", "hash-a")
		Expect(ok).To(BeFalse())
		Expect(readCacheFile()).To(Equal(before))
	})

	It("stale output hash: classifies as rebuild and serves no entries", func() {
		seed := distill.NewFileCache(cachePath, &warnBuf)
		Expect(seed.Load(ctx, outputPath)).To(Succeed())
		seed.Put("a", "hash-a", "- **A.** bullet")
		Expect(seed.Save(ctx, outputPath, []string{"a"})).To(Succeed())

		Expect(os.WriteFile(outputPath, []byte("changed"), 0o600)).To(Succeed())

		warnBuf.Reset()
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheRebuild))
		Expect(warnBuf.String()).To(ContainSubstring("hash mismatch"))
		_, ok := c.Get("a", "hash-a")
		Expect(ok).To(BeFalse())
	})

	It("bound output missing: classifies as cold and serves no entries", func() {
		seed := distill.NewFileCache(cachePath, &warnBuf)
		Expect(seed.Load(ctx, outputPath)).To(Succeed())
		seed.Put("a", "hash-a", "- **A.** bullet")
		Expect(seed.Save(ctx, outputPath, []string{"a"})).To(Succeed())
		Expect(os.Remove(outputPath)).To(Succeed())

		warnBuf.Reset()
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheCold))
		Expect(warnBuf.String()).To(ContainSubstring("missing"))
		_, ok := c.Get("a", "hash-a")
		Expect(ok).To(BeFalse())
	})

	It("unbound cache: rebuilds from an existing output and runs cold without one", func() {
		seed := distill.NewFileCache(cachePath, &warnBuf)
		Expect(seed.Load(ctx, outputPath)).To(Succeed())
		seed.Put("a", "hash-a", "- **A.** bullet")
		Expect(seed.Save(ctx, outputPath, []string{"a"})).To(Succeed())

		var raw map[string]json.RawMessage
		Expect(json.Unmarshal(readCacheFile(), &raw)).To(Succeed())
		delete(raw, "output_path")
		delete(raw, "output_hash")
		unbound, err := json.Marshal(raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(cachePath, unbound, 0o600)).To(Succeed())

		warnBuf.Reset()
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheRebuild))
		Expect(warnBuf.String()).To(ContainSubstring("no output binding"))
		_, ok := c.Get("a", "hash-a")
		Expect(ok).To(BeFalse())

		Expect(os.Remove(outputPath)).To(Succeed())
		warnBuf.Reset()
		c2 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c2.Load(ctx, outputPath)).To(Succeed())
		Expect(c2.Status()).To(Equal(distill.CacheCold))
		Expect(warnBuf.String()).To(ContainSubstring("no output binding"))
	})

	It("SaveMerged preserves the binding recorded by Load", func() {
		seed := distill.NewFileCache(cachePath, &warnBuf)
		Expect(seed.Load(ctx, outputPath)).To(Succeed())
		seed.Put("a", "hash-a", "- **A.** bullet")
		Expect(seed.Save(ctx, outputPath, []string{"a"})).To(Succeed())
		beforePath, beforeHash := readCacheBinding()

		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheWarm))
		c.Put("b", "hash-b", "- **B.** bullet")
		Expect(c.SaveMerged(ctx)).To(Succeed())

		afterPath, afterHash := readCacheBinding()
		Expect(afterPath).To(Equal(beforePath))
		Expect(afterHash).To(Equal(beforeHash))

		var cf struct {
			Entries map[string]json.RawMessage `json:"entries"`
		}
		Expect(json.Unmarshal(readCacheFile(), &cf)).To(Succeed())
		Expect(cf.Entries).To(HaveKey("a"))
		Expect(cf.Entries).To(HaveKey("b"))

		// The stale binding was preserved rather than rebound.
		Expect(os.WriteFile(outputPath, []byte("changed"), 0o600)).To(Succeed())
		c2 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c2.Load(ctx, outputPath)).To(Succeed())
		Expect(c2.Status()).To(Equal(distill.CacheRebuild))
	})

	It("Save returns an error when the output cannot be read", func() {
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		Expect(os.Remove(outputPath)).To(Succeed())

		err := c.Save(ctx, outputPath, []string{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("for cache binding"))
	})

	It("SaveMerged on an unbound cache writes no binding", func() {
		writeCacheFile(`{"version":"v4","entries":{}}`)

		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx, outputPath)).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheRebuild))
		c.Put("a", "hash-a", "- **A.** bullet")
		Expect(c.SaveMerged(ctx)).To(Succeed())

		var raw map[string]json.RawMessage
		Expect(json.Unmarshal(readCacheFile(), &raw)).To(Succeed())
		Expect(raw).NotTo(HaveKey("output_path"))
		Expect(raw).NotTo(HaveKey("output_hash"))
	})
})

var _ = Describe("NoopCache", func() {
	var ctx = context.Background()

	It("Get always misses", func() {
		c := distill.NewNoopCache()
		_, ok := c.Get("any", "hash")
		Expect(ok).To(BeFalse())
	})

	It("Load returns nil and reports cold", func() {
		c := distill.NewNoopCache()
		Expect(c.Load(ctx, "/tmp/out.md")).To(Succeed())
		Expect(c.Status()).To(Equal(distill.CacheCold))
	})

	It("Save returns nil", func() {
		c := distill.NewNoopCache()
		Expect(c.Save(ctx, "/tmp/out.md", []string{"a"})).To(Succeed())
	})

	It("SaveMerged returns nil", func() {
		c := distill.NewNoopCache()
		Expect(c.SaveMerged(ctx)).To(Succeed())
	})

	It("Get still misses after Put (Put is a no-op)", func() {
		c := distill.NewNoopCache()
		c.Put("a", "hash", "- **A.** bullet")
		_, ok := c.Get("a", "hash")
		Expect(ok).To(BeFalse())
	})

	It("RuleHash returns a non-empty string", func() {
		c := distill.NewNoopCache()
		Expect(c.RuleHash("model", "body")).NotTo(BeEmpty())
	})
})
