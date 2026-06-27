// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/distill/mocks"
	"github.com/bborbe/distill/pkg/distill"
)

// stubRunnerWith builds a Counterfeiter-generated DistillRunner whose Run
// returns a body keyed on the rule id embedded in the prompt — keeping the
// behavioural e2e tests readable while still exercising the generated mock.
func stubRunnerWith(bySection map[string]string) *mocks.DistillRunner {
	runner := &mocks.DistillRunner{}
	runner.RunStub = func(_ context.Context, _ string, prompt string) (string, error) {
		for section, body := range bySection {
			if strings.Contains(prompt, "id="+section) {
				return body, nil
			}
		}
		return "- (no match)", nil
	}
	return runner
}

// promptsSeen returns every prompt the runner was called with, in call order.
func promptsSeen(r *mocks.DistillRunner) []string {
	out := make([]string, r.RunCallCount())
	for i := 0; i < r.RunCallCount(); i++ {
		_, _, prompt := r.RunArgsForCall(i)
		out[i] = prompt
	}
	return out
}

var _ = Describe("Driver", func() {
	var (
		ctx       context.Context
		tempDir   string
		sourceDir string
		targetA   string
		targetB   string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tempDir, err = os.MkdirTemp("", "distill-e2e-*")
		Expect(err).NotTo(HaveOccurred())
		sourceDir = filepath.Join(tempDir, "sources")
		Expect(os.Mkdir(sourceDir, 0o755)).To(Succeed())
		targetA = filepath.Join(tempDir, "targetA.md")
		targetB = filepath.Join(tempDir, "targetB.md")
	})

	AfterEach(func() {
		_ = os.RemoveAll(tempDir)
	})

	writeSource := func(name, content string) {
		Expect(os.WriteFile(filepath.Join(sourceDir, name), []byte(content), 0o644)).To(Succeed())
	}
	writeTarget := func(path, content string) {
		Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
	}
	readTarget := func(path string) string {
		b, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		return string(b)
	}

	newDriver := func(stub *mocks.DistillRunner) *distill.Driver {
		return &distill.Driver{
			Parser:   distill.NewParser(),
			Resolver: distill.NewResolver(),
			Scanner:  distill.NewScanner(),
			Runner:   stub,
			Writer:   distill.NewWriter(),
			Stderr:   GinkgoWriter,
		}
	}

	It("compiles one rule into the matching marker block", func() {
		writeSource("rule-a.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n  order: 10\n---\n\nlong-form rule A body\n")
		writeTarget(targetA, "# Top\n\n## Git\n\nsome prose\n\n<!-- begin:distill section=\"Git\" -->\n<!-- end:distill section=\"Git\" -->\n\nafter\n")

		stub := stubRunnerWith(map[string]string{"rule-a": "- compressed rule A"})
		d := newDriver(stub)
		Expect(d.Run(ctx, sourceDir, tempDir)).To(Succeed())

		got := readTarget(targetA)
		Expect(got).To(ContainSubstring("<!-- begin:distill section=\"Git\" -->\n- compressed rule A\n<!-- end:distill section=\"Git\" -->"))
		Expect(got).To(HavePrefix("# Top\n\n## Git\n\nsome prose\n\n"))
		Expect(got).To(HaveSuffix("\nafter\n"))
	})

	It("preserves operator prose outside markers byte-for-byte", func() {
		writeSource("rule-x.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n---\n\nbody\n")
		before := "prose before\n\n<!-- begin:distill section=\"Git\" -->\nstale content\n<!-- end:distill section=\"Git\" -->\n\nprose after\n"
		writeTarget(targetA, before)
		stub := stubRunnerWith(map[string]string{"rule-x": "- new bullet"})
		Expect(newDriver(stub).Run(ctx, sourceDir, tempDir)).To(Succeed())
		got := readTarget(targetA)
		Expect(got).To(HavePrefix("prose before\n\n<!-- begin:distill section=\"Git\" -->\n"))
		Expect(got).To(HaveSuffix("\n<!-- end:distill section=\"Git\" -->\n\nprose after\n"))
		Expect(got).To(ContainSubstring("- new bullet"))
		Expect(got).NotTo(ContainSubstring("stale content"))
	})

	It("groups multiple rules into the same marker block in sort order", func() {
		writeSource("a-rule.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n  order: 20\n  id: rule-late\n---\nbody late\n")
		writeSource("b-rule.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n  order: 10\n  id: rule-early\n---\nbody early\n")
		writeTarget(targetA, "<!-- begin:distill section=\"Git\" -->\n<!-- end:distill section=\"Git\" -->\n")
		stub := stubRunnerWith(map[string]string{"rule-early": "- early; - late"})
		Expect(newDriver(stub).Run(ctx, sourceDir, tempDir)).To(Succeed())
		Expect(promptsSeen(stub)).To(HaveLen(1))
		idxEarly := strings.Index(promptsSeen(stub)[0], "id=rule-early")
		idxLate := strings.Index(promptsSeen(stub)[0], "id=rule-late")
		Expect(idxEarly).To(BeNumerically(">=", 0))
		Expect(idxLate).To(BeNumerically(">", idxEarly))
	})

	It("writes one prompt per (target, section) group", func() {
		writeSource("git1.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n---\nbody1\n")
		writeSource("k8s1.md", "---\ndistill:\n  target: "+targetA+"\n  section: K8s\n---\nbody2\n")
		writeSource("ob1.md", "---\ndistill:\n  target: "+targetB+"\n  section: Obs\n---\nbody3\n")
		writeTarget(targetA, "<!-- begin:distill section=\"Git\" -->\n<!-- end:distill section=\"Git\" -->\n<!-- begin:distill section=\"K8s\" -->\n<!-- end:distill section=\"K8s\" -->\n")
		writeTarget(targetB, "<!-- begin:distill section=\"Obs\" -->\n<!-- end:distill section=\"Obs\" -->\n")
		stub := stubRunnerWith(map[string]string{"git1": "- G", "k8s1": "- K", "ob1": "- O"})
		Expect(newDriver(stub).Run(ctx, sourceDir, tempDir)).To(Succeed())
		Expect(promptsSeen(stub)).To(HaveLen(3))
		Expect(readTarget(targetA)).To(ContainSubstring("- G"))
		Expect(readTarget(targetA)).To(ContainSubstring("- K"))
		Expect(readTarget(targetB)).To(ContainSubstring("- O"))
	})

	It("skips source files without a distill: frontmatter block", func() {
		writeSource("docs.md", "---\ntitle: just docs\n---\nrandom note\n")
		writeSource("rule.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n---\nbody\n")
		writeTarget(targetA, "<!-- begin:distill section=\"Git\" -->\n<!-- end:distill section=\"Git\" -->\n")
		stub := stubRunnerWith(map[string]string{"rule": "- ok"})
		Expect(newDriver(stub).Run(ctx, sourceDir, tempDir)).To(Succeed())
		Expect(promptsSeen(stub)).To(HaveLen(1))
	})

	It("excludes disabled rules from prompts and output", func() {
		writeSource("a.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n  id: keep\n---\nactive body\n")
		writeSource("b.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n  id: drop\n  disabled: true\n---\nignored body\n")
		writeTarget(targetA, "<!-- begin:distill section=\"Git\" -->\n<!-- end:distill section=\"Git\" -->\n")
		stub := stubRunnerWith(map[string]string{"keep": "- only keep"})
		Expect(newDriver(stub).Run(ctx, sourceDir, tempDir)).To(Succeed())
		Expect(promptsSeen(stub)).To(HaveLen(1))
		Expect(promptsSeen(stub)[0]).NotTo(ContainSubstring("id=drop"))
		Expect(promptsSeen(stub)[0]).NotTo(ContainSubstring("ignored body"))
	})

	It("errors when a source target file does not exist", func() {
		writeSource("a.md", "---\ndistill:\n  target: "+filepath.Join(tempDir, "nope.md")+"\n  section: Git\n---\nbody\n")
		stub := stubRunnerWith(nil)
		err := newDriver(stub).Run(ctx, sourceDir, tempDir)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("stat target"))
	})

	It("errors when a source's section has no matching marker pair", func() {
		writeSource("a.md", "---\ndistill:\n  target: "+targetA+"\n  section: NoSuchSection\n---\nbody\n")
		writeTarget(targetA, "<!-- begin:distill section=\"Git\" -->\n<!-- end:distill section=\"Git\" -->\n")
		stub := stubRunnerWith(nil)
		err := newDriver(stub).Run(ctx, sourceDir, tempDir)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no <!-- begin:distill section=\"NoSuchSection\""))
	})

	It("errors on duplicate (target, section, order, id)", func() {
		writeSource("dup1.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n  id: same\n  order: 10\n---\nbody\n")
		writeSource("dup2.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n  id: same\n  order: 10\n---\nbody\n")
		writeTarget(targetA, "<!-- begin:distill section=\"Git\" -->\n<!-- end:distill section=\"Git\" -->\n")
		err := newDriver(stubRunnerWith(nil)).Run(ctx, sourceDir, tempDir)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("duplicate"))
	})

	It("warns and empties a marker block with no source claiming it", func() {
		writeSource("a.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n---\nbody\n")
		writeTarget(targetA, "<!-- begin:distill section=\"Git\" -->\n<!-- end:distill section=\"Git\" -->\n<!-- begin:distill section=\"Orphan\" -->\nold content\n<!-- end:distill section=\"Orphan\" -->\n")
		stub := stubRunnerWith(map[string]string{"a": "- a"})
		Expect(newDriver(stub).Run(ctx, sourceDir, tempDir)).To(Succeed())
		got := readTarget(targetA)
		Expect(got).To(ContainSubstring("<!-- begin:distill section=\"Orphan\" -->\n<!-- end:distill section=\"Orphan\" -->"))
		Expect(got).NotTo(ContainSubstring("old content"))
	})

	It("errors on orphan begin marker", func() {
		writeSource("a.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n---\nbody\n")
		writeTarget(targetA, "<!-- begin:distill section=\"Git\" -->\n(no end)\n")
		err := newDriver(stubRunnerWith(nil)).Run(ctx, sourceDir, tempDir)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("orphan begin marker"))
	})

	It("errors on orphan end marker", func() {
		writeSource("a.md", "---\ndistill:\n  target: "+targetA+"\n  section: Git\n---\nbody\n")
		writeTarget(targetA, "<!-- end:distill section=\"Git\" -->\n")
		err := newDriver(stubRunnerWith(nil)).Run(ctx, sourceDir, tempDir)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("orphan end marker"))
	})
})
