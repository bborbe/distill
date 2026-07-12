// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/distill/pkg/distill"
)

var _ = Describe("ValidateBullet", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("accepts a valid single-line bullet", func() {
		Expect(distill.ValidateBullet(ctx, "rule-a", "- **Prefix.** Body text.")).To(Succeed())
	})

	It("accepts a valid multi-line bullet with an indented fenced code block", func() {
		bullet := "- **Trading: Build.** Always `cd` into the service dir first.\n\n  ```\n  cd svc && make test\n  ```"
		Expect(distill.ValidateBullet(ctx, "build", bullet)).To(Succeed())
	})

	It("accepts a multi-line continuation bullet", func() {
		bullet := "- **Async State Closer.** End with a panel: icon + what,\n  `👤 You:` verb,\n  `⏰ Next:` trigger."
		Expect(distill.ValidateBullet(ctx, "closer", bullet)).To(Succeed())
	})

	It("returns error 'empty bullet' for empty input", func() {
		err := distill.ValidateBullet(ctx, "x", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("empty bullet"))
	})

	It("returns error 'empty bullet' for whitespace-only input", func() {
		err := distill.ValidateBullet(ctx, "x", "   \n\t  ")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("empty bullet"))
	})

	It("returns error 'missing bold prefix' when bullet lacks - ** prefix", func() {
		err := distill.ValidateBullet(ctx, "x", "- plain text without bold")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing bold prefix"))
	})

	It("returns error 'missing bold prefix' when bold span is never closed", func() {
		err := distill.ValidateBullet(ctx, "x", "- **Unclosed prefix without closing asterisks")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing bold prefix"))
	})

	It("returns error naming the count when two column-0 list lines are present", func() {
		bullet := "- **First.** body\n- **Second.** also body"
		err := distill.ValidateBullet(ctx, "x", bullet)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("expected exactly 1 top-level list item"))
		Expect(err.Error()).To(ContainSubstring("2"))
	})

	It("returns error 'unbalanced code fences' for odd number of fence markers", func() {
		bullet := "- **Prefix.** Body.\n  ```\n  only opening fence"
		err := distill.ValidateBullet(ctx, "x", bullet)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unbalanced code fences"))
	})

	It("accepts a bullet with two balanced fence markers (one open + one close)", func() {
		bullet := "- **Prefix.** Body.\n\n  ```\n  code here\n  ```"
		Expect(distill.ValidateBullet(ctx, "x", bullet)).To(Succeed())
	})

	It("returns error 'unbalanced code fences' for three fence markers", func() {
		bullet := "- **Prefix.** Body.\n  ```\n  code\n  ```\n  ```"
		err := distill.ValidateBullet(ctx, "x", bullet)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unbalanced code fences"))
	})

	It("includes the rule id in all error messages", func() {
		err := distill.ValidateBullet(ctx, "my-rule-id", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("my-rule-id"))
	})
})
