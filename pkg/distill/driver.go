// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/bborbe/errors"
)

// Driver runs the distill pipeline against a single source directory and
// writes one fully-regenerated output file.
//
// Fields are exported so callers (and tests) can replace any collaborator with
// a generated mock. Wiring is the responsibility of `pkg/factory`.
type Driver struct {
	// Parser reads source rule files from the source directory.
	Parser Parser
	// Runner sends batch prompts to `claude --print`.
	Runner Runner
	// Writer commits the rendered output file atomically.
	Writer Writer
	// Cache stores and retrieves validated bullets keyed by content hash.
	Cache Cache
	// Stderr receives warning lines and the run-summary line.
	Stderr io.Writer
	// Verbose enables per-chunk prompt + response logging to Stderr.
	Verbose bool
	// Model is the value passed to `claude --model`.
	Model string
	// Title is the optional `# <text>` heading written under the auto-generated
	// warning comment. Empty = no title heading.
	Title string
	// BatchSize is the max number of cache-miss rules per claude invocation.
	// Set by the factory (default 15); not a CLI flag.
	BatchSize int
	// NoCache, when true, bypasses cache load and save (validation/batching/anti-
	// injection still run). Set from --no-cache in cli.
	NoCache bool
}

// Run reads sourceDir, compresses cache-miss rules in batches, and writes the
// assembled output file. The whole output is regenerated every run; any prior
// contents at outputPath are overwritten only on full success.
func (d *Driver) Run(ctx context.Context, sourceDir, outputPath string) error {
	rules, err := d.Parser.Parse(ctx, sourceDir)
	if err != nil {
		return err
	}
	if err := checkDuplicates(ctx, rules); err != nil {
		return err
	}
	enabled := filterEnabled(rules)
	sortRulesGlobal(enabled)

	if !d.NoCache {
		if loadErr := d.Cache.Load(ctx); loadErr != nil {
			fmt.Fprintf(d.Stderr, "distill: cache load warning: %v\n", loadErr)
		}
	}

	bulletByID, err := d.compileBullets(ctx, enabled)
	if err != nil {
		if !d.NoCache {
			if saveErr := d.Cache.SaveMerged(ctx); saveErr != nil {
				fmt.Fprintf(d.Stderr, "distill: cache save warning: %v\n", saveErr)
			}
		}
		return err
	}

	output := assembleOutput(sourceDir, outputPath, d.Title, enabled, bulletByID)
	if err := d.Writer.Write(ctx, outputPath, output); err != nil {
		return err
	}

	if !d.NoCache {
		keepIDs := make([]string, 0, len(enabled))
		for _, r := range enabled {
			keepIDs = append(keepIDs, r.ID)
		}
		if saveErr := d.Cache.Save(ctx, keepIDs); saveErr != nil {
			fmt.Fprintf(d.Stderr, "distill: cache save warning: %v\n", saveErr)
		}
	}

	return nil
}

// compileBullets returns a validated bullet for every enabled rule id, or an
// error naming the ids that stayed unresolved after one scoped retry. Cache
// hits skip the runner; misses are compressed in chunks of at most BatchSize,
// validated per-id, and any missing/invalid ids are retried exactly once.
func (d *Driver) compileBullets(ctx context.Context, rules []Rule) (map[string]string, error) {
	bulletByID := make(map[string]string, len(rules))
	var misses []Rule
	cachedCount := 0

	for _, r := range rules {
		hash := d.Cache.RuleHash(d.Model, r.Body)
		if !d.NoCache {
			if bullet, ok := d.Cache.Get(r.ID, hash); ok {
				bulletByID[r.ID] = bullet
				cachedCount++
				continue
			}
		}
		misses = append(misses, r)
	}

	batchSize := d.BatchSize
	if batchSize <= 0 {
		batchSize = 15
	}

	failingIDs, compiledCount, chunkCount, err := d.compileChunks(
		ctx,
		misses,
		bulletByID,
		batchSize,
	)
	if err != nil {
		return nil, err
	}

	retriedCount := len(failingIDs)
	if retriedCount > 0 {
		ruleByID := make(map[string]Rule, len(rules))
		for _, r := range rules {
			ruleByID[r.ID] = r
		}
		retryRules := make([]Rule, 0, retriedCount)
		for _, id := range failingIDs {
			if r, ok := ruleByID[id]; ok {
				retryRules = append(retryRules, r)
			}
		}
		stillFailing, _, _, retryErr := d.compileChunks(ctx, retryRules, bulletByID, batchSize)
		if retryErr != nil {
			return nil, retryErr
		}
		if len(stillFailing) > 0 {
			return nil, errors.Errorf(ctx, "unresolved rule ids after retry: %v", stillFailing)
		}
	}

	fmt.Fprintf(d.Stderr, "distill: %d cached, %d compiled (%d chunks), %d retried\n",
		cachedCount, compiledCount, chunkCount, retriedCount)

	return bulletByID, nil
}

// compileChunks sends rules in batches of batchSize to the runner, validates
// each returned bullet, and accumulates results into bulletByID. It returns
// the ids that failed to produce a valid bullet, plus counts.
func (d *Driver) compileChunks(
	ctx context.Context,
	rules []Rule,
	bulletByID map[string]string,
	batchSize int,
) (failingIDs []string, compiled int, chunks int, err error) {
	for i := 0; i < len(rules); i += batchSize {
		// Non-blocking context cancellation check between chunks.
		select {
		case <-ctx.Done():
			return nil, 0, 0, errors.Wrapf(ctx, ctx.Err(), "cancelled")
		default:
		}

		end := i + batchSize
		if end > len(rules) {
			end = len(rules)
		}
		chunk := rules[i:end]
		chunks++

		bodies := make([]RuleBody, 0, len(chunk))
		chunkIDs := make([]string, 0, len(chunk))
		for _, r := range chunk {
			bodies = append(bodies, RuleBody{ID: r.ID, Body: r.Body})
			chunkIDs = append(chunkIDs, r.ID)
		}

		prompt, buildErr := BuildBatchPrompt(ctx, bodies)
		if buildErr != nil {
			return nil, 0, chunks, buildErr
		}

		resp, runErr := d.Runner.Run(ctx, d.Model, SystemPrompt(), prompt)
		if runErr != nil {
			return nil, 0, chunks, errors.Wrapf(ctx, runErr, "claude run chunk ids=%v", chunkIDs)
		}

		bullets, warnings, _ := ParseBatchResponse(resp, chunkIDs)
		for _, w := range warnings {
			fmt.Fprintf(d.Stderr, "distill: warning: %s\n", w)
		}

		chunkFailing, chunkCompiled := d.processChunkResults(ctx, chunk, bullets, bulletByID)
		failingIDs = append(failingIDs, chunkFailing...)
		compiled += chunkCompiled
	}
	return failingIDs, compiled, chunks, nil
}

// processChunkResults validates each bullet returned for a chunk and records
// valid ones into bulletByID. It returns the failing ids and the compiled count.
func (d *Driver) processChunkResults(
	ctx context.Context,
	chunk []Rule,
	bullets map[string]string,
	bulletByID map[string]string,
) (failingIDs []string, compiled int) {
	for _, r := range chunk {
		bullet, found := bullets[r.ID]
		if !found {
			failingIDs = append(failingIDs, r.ID)
			continue
		}
		if validateErr := ValidateBullet(ctx, r.ID, bullet); validateErr != nil {
			fmt.Fprintf(
				d.Stderr,
				"distill: warning: validation failed for id=%q: %v\n",
				r.ID,
				validateErr,
			)
			failingIDs = append(failingIDs, r.ID)
			continue
		}
		bulletByID[r.ID] = bullet
		hash := d.Cache.RuleHash(d.Model, r.Body)
		d.Cache.Put(r.ID, hash, bullet)
		compiled++
	}
	return
}

func filterEnabled(rules []Rule) []Rule {
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.Disabled {
			continue
		}
		out = append(out, r)
	}
	return out
}

// sortRulesGlobal sorts rules in (section order, order, id) order where section
// order is determined by the minimum rule.Order within each section, with ties
// broken alphabetically.
func sortRulesGlobal(rules []Rule) {
	_, sectionOrder := groupBySection(rules)
	sectionRank := make(map[string]int, len(sectionOrder))
	for i, s := range sectionOrder {
		sectionRank[s] = i
	}
	sort.SliceStable(rules, func(i, j int) bool {
		a, b := rules[i], rules[j]
		ra, rb := sectionRank[a.Section], sectionRank[b.Section]
		if ra != rb {
			return ra < rb
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.ID < b.ID
	})
}

// groupBySection returns rules grouped by section AND the section order. A
// section's position is determined by the minimum `Order` among its rules,
// with ties broken alphabetically.
func groupBySection(rules []Rule) (map[string][]Rule, []string) {
	groups := map[string][]Rule{}
	minOrder := map[string]int{}
	for _, r := range rules {
		groups[r.Section] = append(groups[r.Section], r)
		if existing, ok := minOrder[r.Section]; !ok || r.Order < existing {
			minOrder[r.Section] = r.Order
		}
	}
	sections := make([]string, 0, len(groups))
	for section := range groups {
		sections = append(sections, section)
	}
	sort.Slice(sections, func(i, j int) bool {
		a, b := sections[i], sections[j]
		if minOrder[a] != minOrder[b] {
			return minOrder[a] < minOrder[b]
		}
		return a < b
	})
	return groups, sections
}

// assembleOutput builds the complete output string from validated per-rule bullets.
// Go alone determines sections, ordering, and the header; the model contributes
// only per-id bullet text.
func assembleOutput(
	sourceDir, outputPath, title string,
	rules []Rule,
	bulletByID map[string]string,
) string {
	var b strings.Builder
	b.WriteString("<!--\n")
	b.WriteString("  AUTO-GENERATED by distill — do not edit by hand.\n")
	fmt.Fprintf(&b, "  Source: %s\n", sourceDir)
	fmt.Fprintf(&b, "  Regenerate: distill --source %s --output %s\n", sourceDir, outputPath)
	b.WriteString("-->\n")
	if title != "" {
		b.WriteString("\n# ")
		b.WriteString(title)
		b.WriteString("\n")
	}

	groups, sectionOrder := groupBySection(rules)

	for _, section := range sectionOrder {
		b.WriteString("\n## ")
		b.WriteString(section)
		b.WriteString("\n\n")
		for _, r := range groups[section] {
			bullet := strings.TrimRight(bulletByID[r.ID], "\n \t")
			if bullet != "" {
				b.WriteString(bullet)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func checkDuplicates(ctx context.Context, rules []Rule) error {
	type key struct {
		s string
		o int
		i string
	}
	seen := map[key]string{}
	for _, r := range rules {
		k := key{r.Section, r.Order, r.ID}
		if prev, ok := seen[k]; ok {
			return errors.Errorf(ctx,
				"duplicate (section=%q, order=%d, id=%q) in %q and %q",
				r.Section, r.Order, r.ID, prev, r.Path)
		}
		seen[k] = r.Path
	}
	return nil
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
