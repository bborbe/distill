// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/distill/pkg/distill"
	"github.com/bborbe/distill/pkg/factory"
)

var _ = Describe("CreateDriver", func() {
	It("wires all collaborators with the supplied options", func() {
		var stderr bytes.Buffer
		cache := distill.NewNoopCache()
		d := factory.CreateDriver(&stderr, cache, "claude-haiku-4-5", "My Title", true)
		Expect(d).NotTo(BeNil())
		Expect(d.Parser).NotTo(BeNil())
		Expect(d.Runner).NotTo(BeNil())
		Expect(d.Writer).NotTo(BeNil())
		Expect(d.Cache).NotTo(BeNil())
		Expect(d.BatchSize).To(Equal(15))
		Expect(d.Stderr).To(Equal(&stderr))
		Expect(d.Model).To(Equal("claude-haiku-4-5"))
		Expect(d.Title).To(Equal("My Title"))
		Expect(d.Verbose).To(BeTrue())
	})
})
