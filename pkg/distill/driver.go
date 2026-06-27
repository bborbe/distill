// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/bborbe/errors"
)

// Driver runs the distill pipeline against a single source directory.
//
// Fields are exported so callers (and tests) can replace any collaborator with
// a generated mock. Wiring is the responsibility of `pkg/factory`.
type Driver struct {
	// Parser reads source rule files from the source directory.
	Parser Parser
	// Resolver maps each rule's `target:` to an absolute filesystem path.
	Resolver Resolver
	// Scanner partitions target files into prose / marker regions.
	Scanner Scanner
	// Runner sends per-group prompts to `claude --print`.
	Runner Runner
	// Writer commits the rendered target file atomically.
	Writer Writer
	// Stderr receives verbose-mode prompt/response dumps and warning lines.
	Stderr io.Writer
	// Verbose enables per-group prompt + response logging to Stderr.
	Verbose bool
	// Model is the value passed to `claude --model`.
	Model string
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

func (d *Driver) processTarget(ctx context.Context, absPath string, groups map[string][]Rule) error {
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
	for _, section := range Sections(regions) {
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
	out := Render(regions, compressed)
	if err := d.Writer.Write(ctx, absPath, out); err != nil {
		return err
	}
	return nil
}

func (d *Driver) runGroup(ctx context.Context, absPath, section string, rules []Rule) (string, error) {
	bodies := make([]RuleBody, 0, len(rules))
	for _, r := range rules {
		bodies = append(bodies, RuleBody{ID: r.ID, Body: r.Body})
	}
	prompt := BuildPrompt(bodies)
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

func filterEnabled(rules []Rule) []Rule {
	out := rules[:0:0]
	for _, r := range rules {
		if r.Disabled {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (d *Driver) resolveAndGroup(ctx context.Context, rules []Rule, cwd string) (map[string]map[string][]Rule, error) {
	out := map[string]map[string][]Rule{}
	for _, r := range rules {
		abs, err := d.Resolver.Resolve(ctx, r.Target, cwd)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, errors.Wrapf(ctx, err, "stat target %q (resolved from %q)", abs, r.Target)
		}
		if _, ok := out[abs]; !ok {
			out[abs] = map[string][]Rule{}
		}
		out[abs][r.Section] = append(out[abs][r.Section], r)
	}
	return out, nil
}

func checkDuplicates(ctx context.Context, rules []Rule) error {
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

func sectionSet(regions []Region) map[string]bool {
	out := map[string]bool{}
	for _, r := range regions {
		if r.IsMarker {
			out[r.Section] = true
		}
	}
	return out
}

func sortedKeys(m map[string]map[string][]Rule) []string {
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
