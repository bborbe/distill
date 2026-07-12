// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill

import (
	"os"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("buildClaudeArgs", func() {
	Context("with a model", func() {
		var args []string

		BeforeEach(func() {
			args = buildClaudeArgs("claude-3-5-sonnet", "the system prompt text")
		})

		It("contains --setting-sources followed by empty string", func() {
			idx := slices.Index(args, "--setting-sources")
			Expect(idx).To(BeNumerically(">=", 0))
			Expect(idx + 1).To(BeNumerically("<", len(args)))
			Expect(args[idx+1]).To(Equal(""))
		})

		It("contains --tools followed by empty string", func() {
			idx := slices.Index(args, "--tools")
			Expect(idx).To(BeNumerically(">=", 0))
			Expect(idx + 1).To(BeNumerically("<", len(args)))
			Expect(args[idx+1]).To(Equal(""))
		})

		It("contains --disable-slash-commands", func() {
			Expect(slices.Contains(args, "--disable-slash-commands")).To(BeTrue())
		})

		It("contains --no-session-persistence", func() {
			Expect(slices.Contains(args, "--no-session-persistence")).To(BeTrue())
		})

		It("contains --strict-mcp-config", func() {
			Expect(slices.Contains(args, "--strict-mcp-config")).To(BeTrue())
		})

		It("contains --system-prompt followed by the systemPrompt string inline", func() {
			idx := slices.Index(args, "--system-prompt")
			Expect(idx).To(BeNumerically(">=", 0))
			Expect(idx + 1).To(BeNumerically("<", len(args)))
			Expect(args[idx+1]).To(Equal("the system prompt text"))
		})

		It("includes --model when model is non-empty", func() {
			idx := slices.Index(args, "--model")
			Expect(idx).To(BeNumerically(">=", 0))
			Expect(idx + 1).To(BeNumerically("<", len(args)))
			Expect(args[idx+1]).To(Equal("claude-3-5-sonnet"))
		})
	})

	Context("without a model", func() {
		var args []string

		BeforeEach(func() {
			args = buildClaudeArgs("", "sp")
		})

		It("omits --model", func() {
			Expect(slices.Contains(args, "--model")).To(BeFalse())
		})
	})
})

var _ = Describe("neutralDir", func() {
	It("returns os.TempDir()", func() {
		Expect(neutralDir()).To(Equal(os.TempDir()))
	})
})
