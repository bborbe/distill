// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill_test

import (
	"bytes"
	"context"
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

var _ = Describe("FileCache", func() {
	var (
		ctx       context.Context
		cacheDir  string
		cachePath string
		warnBuf   bytes.Buffer
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		cacheDir, err = os.MkdirTemp("", "distill-cache-unit-*")
		Expect(err).NotTo(HaveOccurred())
		cachePath = filepath.Join(cacheDir, "test-cache.json")
		warnBuf.Reset()
	})

	AfterEach(func() {
		_ = os.RemoveAll(cacheDir)
	})

	It("Load on missing file warns and runs cold (Get always misses)", func() {
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx)).To(Succeed())
		Expect(warnBuf.String()).To(ContainSubstring("missing"))
		_, ok := c.Get("any", "hash")
		Expect(ok).To(BeFalse())
	})

	It("Load on corrupt JSON warns and runs cold", func() {
		Expect(os.WriteFile(cachePath, []byte("{not json"), 0o600)).To(Succeed())
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx)).To(Succeed())
		Expect(warnBuf.String()).To(ContainSubstring("corrupt"))
		_, ok := c.Get("any", "hash")
		Expect(ok).To(BeFalse())
	})

	It("Load on version-mismatch warns and ignores persisted entries", func() {
		data, err := json.Marshal(map[string]interface{}{
			"version": "v0",
			"entries": map[string]interface{}{
				"a": map[string]string{"hash": "h", "bullet": "- **A.** text"},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(cachePath, data, 0o600)).To(Succeed())

		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx)).To(Succeed())
		Expect(warnBuf.String()).To(ContainSubstring("mismatch"))
		_, ok := c.Get("a", "h")
		Expect(ok).To(BeFalse())
	})

	It("round-trip: Put + Save → Load → Get hits", func() {
		c := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c.Load(ctx)).To(Succeed())
		c.Put("a", "hash-a", "- **A.** bullet a")
		c.Put("b", "hash-b", "- **B.** bullet b")
		Expect(c.Save(ctx, []string{"a", "b"})).To(Succeed())

		c2 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c2.Load(ctx)).To(Succeed())

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
		Expect(c1.Load(ctx)).To(Succeed())
		c1.Put("a", "hash-a", "- **A.** bullet")
		c1.Put("b", "hash-b", "- **B.** bullet")
		Expect(c1.Save(ctx, []string{"a", "b"})).To(Succeed())

		// Save with only "a" — prunes b.
		c2 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c2.Load(ctx)).To(Succeed())
		Expect(c2.Save(ctx, []string{"a"})).To(Succeed())

		// Reload and verify a present, b absent.
		c3 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c3.Load(ctx)).To(Succeed())
		_, okA := c3.Get("a", "hash-a")
		Expect(okA).To(BeTrue())
		_, okB := c3.Get("b", "hash-b")
		Expect(okB).To(BeFalse())
	})

	It("SaveMerged: no pruning — loaded + Put all persist", func() {
		// Seed with a and b.
		c1 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c1.Load(ctx)).To(Succeed())
		c1.Put("a", "hash-a", "- **A.** bullet")
		c1.Put("b", "hash-b", "- **B.** bullet")
		Expect(c1.Save(ctx, []string{"a", "b"})).To(Succeed())

		// Load a,b; Put c; SaveMerged.
		c2 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c2.Load(ctx)).To(Succeed())
		c2.Put("c", "hash-c", "- **C.** bullet")
		Expect(c2.SaveMerged(ctx)).To(Succeed())

		// Reload and verify all three present.
		c3 := distill.NewFileCache(cachePath, &warnBuf)
		Expect(c3.Load(ctx)).To(Succeed())
		_, okA := c3.Get("a", "hash-a")
		Expect(okA).To(BeTrue())
		_, okB := c3.Get("b", "hash-b")
		Expect(okB).To(BeTrue())
		_, okC := c3.Get("c", "hash-c")
		Expect(okC).To(BeTrue())
	})
})

var _ = Describe("NoopCache", func() {
	var ctx = context.Background()

	It("Get always misses", func() {
		c := distill.NewNoopCache()
		_, ok := c.Get("any", "hash")
		Expect(ok).To(BeFalse())
	})

	It("Load returns nil", func() {
		c := distill.NewNoopCache()
		Expect(c.Load(ctx)).To(Succeed())
	})

	It("Save returns nil", func() {
		c := distill.NewNoopCache()
		Expect(c.Save(ctx, []string{"a"})).To(Succeed())
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
