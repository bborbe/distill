// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cli wires the source parser, target resolver, marker scanner, claude
// runner, and writer into the `distill` driver.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/bborbe/errors"

	"github.com/bborbe/distill/pkg/claude"
	"github.com/bborbe/distill/pkg/marker"
	"github.com/bborbe/distill/pkg/prompts"
	"github.com/bborbe/distill/pkg/source"
	"github.com/bborbe/distill/pkg/target"
	"github.com/bborbe/distill/pkg/writer"
)

// Driver runs the distill pipeline against a single source directory.
type Driver struct {
	Parser   source.Parser
	Resolver target.Resolver
	Scanner  marker.Scanner
	Runner   claude.Runner
	Writer   writer.Writer
	Stderr   io.Writer
	Verbose  bool
	Model    string
}

// NewDriver returns a Driver with default real implementations of each
// collaborator.
func NewDriver(stderr io.Writer, model string, verbose bool) *Driver {
	return &Driver{
		Parser:   source.NewParser(),
		Resolver: target.NewResolver(),
		Scanner:  marker.NewScanner(),
		Runner:   claude.NewRunner(),
		Writer:   writer.NewWriter(),
		Stderr:   stderr,
		Verbose:  verbose,
		Model:    model,
	}
}

// Run reads sourceDir, groups rules by (target, section), invokes the runner
// once per group, and writes the result between matching markers in each
// resolved target file. cwd is the working directory used to resolve relative
// `target:` strings.
func (d *Driver) Run(ctx context.Context, sourceDir string, cwd string) error {
	rules, err := d.Parser.Parse(ctx, sourceDir)
	if err != nil {
		return err
	}
	if err := checkDuplicates(ctx, rules); err != nil {
		return err
	}
	enabled := filterEnabled(rules)
	groupsByTarget, err := d.resolveAndGroup(ctx, enabled, cwd)
	if err != nil {
		return err
	}
	targets := sortedKeys(groupsByTarget)
	for _, absPath := range targets {
		if err := d.processTarget(ctx, absPath, groupsByTarget[absPath]); err != nil {
			return err
		}
	}
	return nil
}

func (d *Driver) processTarget(ctx context.Context, absPath string, groups map[string][]source.Rule) error {
	contentBytes, err := os.ReadFile(absPath)
	if err != nil {
		return errors.Wrapf(ctx, err, "read target %q", absPath)
	}
	regions, err := d.Scanner.Scan(ctx, absPath, string(contentBytes))
	if err != nil {
		return err
	}
	targetSections := sectionSet(regions)
	for section := range groups {
		if !targetSections[section] {
			return errors.Errorf(ctx,
				"target %q has no <!-- begin:distill section=%q --> marker for source rule(s) declaring it",
				absPath, section)
		}
	}
	compressed := map[string]string{}
	for _, section := range marker.Sections(regions) {
		rules := groups[section]
		if len(rules) == 0 {
			fmt.Fprintf(d.Stderr,
				"warning: target %q section=%q has marker pair but no source rule; emitting empty block\n",
				absPath, section)
			compressed[section] = ""
			continue
		}
		body, err := d.runGroup(ctx, absPath, section, rules)
		if err != nil {
			return err
		}
		compressed[section] = body
	}
	out := marker.Render(regions, compressed)
	if err := d.Writer.Write(ctx, absPath, out); err != nil {
		return err
	}
	return nil
}

func (d *Driver) runGroup(ctx context.Context, absPath, section string, rules []source.Rule) (string, error) {
	bodies := make([]prompts.RuleBody, 0, len(rules))
	for _, r := range rules {
		bodies = append(bodies, prompts.RuleBody{ID: r.ID, Body: r.Body})
	}
	prompt := prompts.Build(bodies)
	if d.Verbose {
		fmt.Fprintf(d.Stderr, "\n--- distill prompt target=%q section=%q ---\n%s\n", absPath, section, prompt)
	}
	body, err := d.Runner.Run(ctx, d.Model, prompt)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "claude run target=%q section=%q", absPath, section)
	}
	if d.Verbose {
		fmt.Fprintf(d.Stderr, "\n--- distill response target=%q section=%q ---\n%s\n", absPath, section, body)
	}
	return body, nil
}

func filterEnabled(rules []source.Rule) []source.Rule {
	out := rules[:0:0]
	for _, r := range rules {
		if r.Disabled {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (d *Driver) resolveAndGroup(ctx context.Context, rules []source.Rule, cwd string) (map[string]map[string][]source.Rule, error) {
	out := map[string]map[string][]source.Rule{}
	for _, r := range rules {
		abs, err := d.Resolver.Resolve(ctx, r.Target, cwd)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, errors.Wrapf(ctx, err, "stat target %q (resolved from %q)", abs, r.Target)
		}
		if _, ok := out[abs]; !ok {
			out[abs] = map[string][]source.Rule{}
		}
		out[abs][r.Section] = append(out[abs][r.Section], r)
	}
	return out, nil
}

func checkDuplicates(ctx context.Context, rules []source.Rule) error {
	type key struct {
		t, s string
		o    int
		i    string
	}
	seen := map[key]string{}
	for _, r := range rules {
		k := key{r.Target, r.Section, r.Order, r.ID}
		if prev, ok := seen[k]; ok {
			return errors.Errorf(ctx,
				"duplicate (target=%q, section=%q, order=%d, id=%q) in %q and %q",
				r.Target, r.Section, r.Order, r.ID, prev, r.Path)
		}
		seen[k] = r.Path
	}
	return nil
}

func sectionSet(regions []marker.Region) map[string]bool {
	out := map[string]bool{}
	for _, r := range regions {
		if r.IsMarker {
			out[r.Section] = true
		}
	}
	return out
}

func sortedKeys(m map[string]map[string][]source.Rule) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ExitCode maps an error returned from Run to a numeric exit code per
// docs/spec.md "Error Cases". Nil → 0, anything else → 1. Exit code 2 (usage)
// is the caller's concern in main.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	return 1
}
