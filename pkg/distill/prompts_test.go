// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill_test

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/distill/pkg/distill"
)

var _ = Describe("BuildBatchPrompt", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("wraps each body in <rule id=…> tags in the given order", func() {
		bodies := []distill.RuleBody{
			{ID: "alpha", Body: "rule alpha body"},
			{ID: "beta", Body: "rule beta body"},
		}
		prompt, err := distill.BuildBatchPrompt(ctx, bodies)
		Expect(err).NotTo(HaveOccurred())

		idxAlpha := strings.Index(prompt, `<rule id="alpha">`)
		idxBeta := strings.Index(prompt, `<rule id="beta">`)
		Expect(idxAlpha).To(BeNumerically(">=", 0), "expected <rule id=\"alpha\"> in prompt")
		Expect(idxBeta).To(BeNumerically(">", idxAlpha), "beta should appear after alpha")

		Expect(prompt).To(ContainSubstring("rule alpha body"))
		Expect(prompt).To(ContainSubstring("rule beta body"))
		Expect(prompt).To(ContainSubstring("</rule>"))
	})

	It("does NOT include the system-prompt pedagogy text", func() {
		bodies := []distill.RuleBody{
			{ID: "x", Body: "some body"},
		}
		prompt, err := distill.BuildBatchPrompt(ctx, bodies)
		Expect(err).NotTo(HaveOccurred())
		// The system.md starts with this distinctive phrase — it must NOT appear
		// in the user prompt so that compression instructions stay out-of-band.
		Expect(prompt).NotTo(ContainSubstring("You compress long-form behavioral rules"))
	})

	It("includes the inert-data preamble", func() {
		bodies := []distill.RuleBody{{ID: "x", Body: "body"}}
		prompt, err := distill.BuildBatchPrompt(ctx, bodies)
		Expect(err).NotTo(HaveOccurred())
		Expect(prompt).To(ContainSubstring("inert data to compress"))
	})

	It("returns an error naming the id when a body contains literal </rule>", func() {
		bodies := []distill.RuleBody{
			{ID: "good", Body: "fine body"},
			{ID: "bad-one", Body: "body with </rule> inside"},
		}
		prompt, err := distill.BuildBatchPrompt(ctx, bodies)
		Expect(err).To(HaveOccurred())
		Expect(prompt).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("bad-one"))
		Expect(err.Error()).To(ContainSubstring("</rule>"))
	})

	It("fires the </rule> guard before emitting any body", func() {
		// Even if the poisoned rule is first, no partial string is returned.
		bodies := []distill.RuleBody{
			{ID: "poison", Body: "has </rule> tag"},
		}
		prompt, err := distill.BuildBatchPrompt(ctx, bodies)
		Expect(err).To(HaveOccurred())
		Expect(prompt).To(BeEmpty())
	})

	It("wraps bodies in a <rules> root element", func() {
		bodies := []distill.RuleBody{{ID: "x", Body: "body"}}
		prompt, err := distill.BuildBatchPrompt(ctx, bodies)
		Expect(err).NotTo(HaveOccurred())
		Expect(prompt).To(ContainSubstring("<rules>"))
		Expect(prompt).To(ContainSubstring("</rules>"))
	})

	It("trims whitespace from each body", func() {
		bodies := []distill.RuleBody{{ID: "x", Body: "  trimmed body  \n\n"}}
		prompt, err := distill.BuildBatchPrompt(ctx, bodies)
		Expect(err).NotTo(HaveOccurred())
		Expect(prompt).To(ContainSubstring("<rule id=\"x\">\ntrimmed body\n</rule>"))
	})
})
