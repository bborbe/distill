// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/distill/pkg/distill"
)

var _ = Describe("ParseBatchResponse", func() {
	It("round-trips two ids in order and returns correct map", func() {
		response := "--- bullet id=foo ---\n- **Foo.** compressed foo\n--- bullet id=bar ---\n- **Bar.** compressed bar\n"
		result, warnings, err := distill.ParseBatchResponse(response, []string{"foo", "bar"})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveKey("foo"))
		Expect(result).To(HaveKey("bar"))
		Expect(result["foo"]).To(Equal("- **Foo.** compressed foo"))
		Expect(result["bar"]).To(Equal("- **Bar.** compressed bar"))
		Expect(warnings).To(BeEmpty())
	})

	It("treats a delimiter line inside a fenced code block as content, not a delimiter", func() {
		// The rule body for "outer" contains a fenced block with a fake delimiter.
		response := strings.Join([]string{
			"--- bullet id=outer ---",
			"- **Outer.** See fence:",
			"  ```",
			"  --- bullet id=fake ---",
			"  not a real delimiter",
			"  ```",
			"--- bullet id=second ---",
			"- **Second.** real second bullet",
			"",
		}, "\n")
		result, warnings, err := distill.ParseBatchResponse(response, []string{"outer", "second"})
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(BeEmpty())
		Expect(result).To(HaveKey("outer"))
		Expect(result).To(HaveKey("second"))
		// The fake delimiter should appear verbatim in outer's body.
		Expect(result["outer"]).To(ContainSubstring("--- bullet id=fake ---"))
		// "second" should be a separate block, not part of outer.
		Expect(result["second"]).To(Equal("- **Second.** real second bullet"))
	})

	It("tolerates stray preamble before the first delimiter and returns a warning", func() {
		response := "Here are the bullets:\n\n--- bullet id=x ---\n- **X.** body\n"
		result, warnings, err := distill.ParseBatchResponse(response, []string{"x"})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveKey("x"))
		Expect(result["x"]).To(Equal("- **X.** body"))
		Expect(warnings).NotTo(BeEmpty())
		Expect(warnings[0]).To(ContainSubstring("preamble"))
	})

	It("drops a bullet addressed to an id not in requestedIDs and warns", func() {
		response := "--- bullet id=wanted ---\n- **Wanted.** ok\n--- bullet id=extra ---\n- **Extra.** unexpected\n"
		result, warnings, err := distill.ParseBatchResponse(response, []string{"wanted"})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveKey("wanted"))
		Expect(result).NotTo(HaveKey("extra"))
		Expect(warnings).NotTo(BeEmpty())
		Expect(warnings[0]).To(ContainSubstring("extra"))
	})

	It("returns empty map and nil error when response contains zero delimiters", func() {
		response := "This response has no delimiters at all.\nJust plain text.\n"
		result, warnings, err := distill.ParseBatchResponse(response, []string{"a", "b"})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeEmpty())
		// No preamble warning is emitted for the zero-delimiter case.
		_ = warnings
	})

	It("returns empty map and nil error for empty response", func() {
		result, _, err := distill.ParseBatchResponse("", []string{"a"})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeEmpty())
	})

	It("trims trailing whitespace from each bullet body", func() {
		response := "--- bullet id=x ---\n- **X.** body\n\n\n"
		result, _, err := distill.ParseBatchResponse(response, []string{"x"})
		Expect(err).NotTo(HaveOccurred())
		Expect(result["x"]).NotTo(HaveSuffix("\n"))
	})

	It("handles a single blank line between delimiter and bullet gracefully", func() {
		response := "--- bullet id=x ---\n\n- **X.** body\n"
		result, _, err := distill.ParseBatchResponse(response, []string{"x"})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveKey("x"))
		Expect(result["x"]).To(ContainSubstring("- **X.** body"))
	})
})
