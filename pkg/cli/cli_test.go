// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/distill/pkg/cli"
)

var _ = Describe("Run", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("missing required flags", func() {
		It("returns a UsageError when both --source and --output are omitted", func() {
			err := cli.Run(ctx, []string{})
			Expect(err).To(HaveOccurred())
			var ue *cli.UsageError
			Expect(errors.As(err, &ue)).To(BeTrue(),
				"expected *cli.UsageError, got %T: %v", err, err)
		})

		It("returns a UsageError when --output is omitted", func() {
			err := cli.Run(ctx, []string{"--source", "/tmp"})
			Expect(err).To(HaveOccurred())
			var ue *cli.UsageError
			Expect(errors.As(err, &ue)).To(BeTrue(),
				"expected *cli.UsageError, got %T: %v", err, err)
		})

		It("returns a UsageError when --source is omitted", func() {
			err := cli.Run(ctx, []string{"--output", "/tmp/out.md"})
			Expect(err).To(HaveOccurred())
			var ue *cli.UsageError
			Expect(errors.As(err, &ue)).To(BeTrue(),
				"expected *cli.UsageError, got %T: %v", err, err)
		})
	})

	Context("runtime failure", func() {
		It("does NOT return a UsageError when flags are present but source dir is missing", func() {
			err := cli.Run(ctx, []string{
				"--source", "/nonexistent/cli-test-dir-distill-99",
				"--output", "/tmp/cli-test-out.md",
			})
			Expect(err).To(HaveOccurred())
			var ue *cli.UsageError
			Expect(errors.As(err, &ue)).To(BeFalse(),
				"expected runtime error, not *cli.UsageError")
		})
	})

	Context("version", func() {
		It("prints the version and exits 0 with no other flags", func() {
			var stdout bytes.Buffer
			err := cli.RunWithWriters(ctx, []string{"--version"}, &stdout, io.Discard)
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout.String()).To(MatchRegexp(`^distill \S+\n$`),
				"--source/--output are required, but --version must bypass them")
		})

		It("prints dev when no ldflag was applied", func() {
			var stdout bytes.Buffer
			Expect(cli.RunWithWriters(ctx, []string{"--version"}, &stdout, io.Discard)).
				To(Succeed())
			Expect(stdout.String()).To(Equal("distill dev\n"),
				"a binary built without -X must report dev, never an empty line")
		})

		It("accepts -v as the shorthand", func() {
			var stdout bytes.Buffer
			Expect(cli.RunWithWriters(ctx, []string{"-v"}, &stdout, io.Discard)).To(Succeed())
			Expect(stdout.String()).To(Equal("distill dev\n"))
		})
	})
})
