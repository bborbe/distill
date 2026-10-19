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
	normalizedOutput, err := normalizeOutputPath(ctx, outputPath)
	if err != nil {
		return err
	}

	rules, err := d.Parser.Parse(ctx, sourceDir)
	if err != nil {
		return err
	}
	if err := checkDuplicates(ctx, rules); err != nil {
		return err
	}
	enabled := filterEnabled(rules)
	sortRulesGlobal(enabled)

	status := CacheCold
	if !d.NoCache {
		status = d.loadCache(ctx, normalizedOutput)
		if status == CacheRebuild {
			if d.tryRebuild(ctx, normalizedOutput, rules, enabled) {
				return nil
			}
			status = CacheCold
		}
	}

	bulletByID, err := d.compileBullets(ctx, enabled)
	if err != nil {
		d.saveMergedUnlessForeign(ctx, status)
		return err
	}

	output := assembleOutput(sourceDir, normalizedOutput, d.Title, enabled, bulletByID)
	if err := d.Writer.Write(ctx, normalizedOutput, output); err != nil {
		return err
	}

	d.saveUnlessForeign(ctx, status, normalizedOutput, enabled)

	return nil
}

// loadCache loads the cache and returns its classification. A load error is a
// warning; it never aborts the run.
func (d *Driver) loadCache(ctx context.Context, outputPath string) CacheStatus {
	if loadErr := d.Cache.Load(ctx, outputPath); loadErr != nil {
		fmt.Fprintf(d.Stderr, "distill: cache load warning: %v\n", loadErr)
		return CacheCold
	}
	return d.Cache.Status()
}

// saveUnlessForeign persists the cache after a successful run. A foreign output
// is never written; --no-cache never writes.
func (d *Driver) saveUnlessForeign(
	ctx context.Context,
	status CacheStatus,
	outputPath string,
	enabled []Rule,
) {
	if d.NoCache || status == CacheForeign {
		return
	}
	if saveErr := d.Cache.Save(ctx, outputPath, ruleIDs(enabled)); saveErr != nil {
		fmt.Fprintf(d.Stderr, "distill: cache save warning: %v\n", saveErr)
	}
}

// saveMergedUnlessForeign merges validated bullets after a failed run. A foreign
// output is never written; --no-cache never writes.
func (d *Driver) saveMergedUnlessForeign(ctx context.Context, status CacheStatus) {
	if d.NoCache || status == CacheForeign {
		return
	}
	if saveErr := d.Cache.SaveMerged(ctx); saveErr != nil {
		fmt.Fprintf(d.Stderr, "distill: cache save warning: %v\n", saveErr)
	}
}

// ruleIDs returns the ids of rules in order.
func ruleIDs(rules []Rule) []string {
	ids := make([]string, 0, len(rules))
	for _, r := range rules {
		ids = append(ids, r.ID)
	}
	return ids
}

// tryRebuild rebuilds cache entries from the committed output. It returns true
// when the run is complete: the cache is saved and the output is untouched.
func (d *Driver) tryRebuild(
	ctx context.Context,
	outputPath string,
	allRules, enabled []Rule,
) bool {
	bulletByID, err := rebuildFromOutput(ctx, outputPath, allRules, enabled)
	if err != nil {
		fmt.Fprintf(
			d.Stderr,
			"distill: warning: committed output %q cannot be attributed "+
				"to the rules: %v — compiling cold\n",
			outputPath,
			err,
		)
		return false
	}
	for _, r := range enabled {
		d.Cache.Put(r.ID, d.Cache.RuleHash(d.Model, r.Body), bulletByID[r.ID])
	}
	if saveErr := d.Cache.Save(ctx, outputPath, ruleIDs(enabled)); saveErr != nil {
		fmt.Fprintf(d.Stderr, "distill: cache save warning: %v\n", saveErr)
	}
	fmt.Fprintf(
		d.Stderr,
		"distill: %d cached, 0 compiled (0 chunks), 0 retried\n",
		len(enabled),
	)
	return true
}

// outputSection is one `## <name>` heading and its bullets in file order.
type outputSection struct {
	Name    string
	Bullets []string
}

// parseOutputSections parses an assembled output file into sections and their
// bullets. It reads only column-0 markers outside fenced code blocks.
func parseOutputSections(ctx context.Context, output string) ([]outputSection, error) {
	p := &outputParser{seen: make(map[string]struct{}, 8)}
	for _, line := range strings.Split(output, "\n") {
		if err := p.consume(ctx, line); err != nil {
			return nil, err
		}
	}
	p.finalizeSection()
	return p.sections, nil
}

// outputParser accumulates sections and bullets while scanning the output.
type outputParser struct {
	sections []outputSection
	seen     map[string]struct{}
	current  *outputSection
	bullet   string
	inFence  bool
}

// consume processes one line: fence toggling first, then markers, then any
// continuation of the open bullet.
func (p *outputParser) consume(ctx context.Context, line string) error {
	if strings.HasPrefix(strings.TrimSpace(line), "```") {
		p.inFence = !p.inFence
	}
	if !p.inFence {
		handled, err := p.marker(ctx, line)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
	}
	if p.bullet != "" {
		p.bullet += "\n" + line
	}
	return nil
}

// marker classifies a column-0 marker line. The bool reports whether the line
// was a marker and therefore must not be appended to the current bullet.
func (p *outputParser) marker(ctx context.Context, line string) (bool, error) {
	switch {
	case strings.HasPrefix(line, "## "):
		return true, p.startSection(ctx, line)
	case strings.HasPrefix(line, "- **"):
		if p.current == nil {
			return false, errors.Errorf(ctx, "bullet before first section heading")
		}
		p.finalizeBullet()
		p.bullet = line
		return true, nil
	case strings.HasPrefix(line, "- "):
		return false, errors.Errorf(ctx, "column-0 list item is not a bold bullet")
	}
	return false, nil
}

// startSection finalizes the open section and opens the heading's section.
func (p *outputParser) startSection(ctx context.Context, line string) error {
	p.finalizeSection()
	name := strings.TrimSpace(strings.TrimPrefix(line, "## "))
	if _, ok := p.seen[name]; ok {
		return errors.Errorf(ctx, "duplicate section heading %q", name)
	}
	p.seen[name] = struct{}{}
	p.current = &outputSection{Name: name}
	return nil
}

// finalizeBullet appends the open bullet, trimmed like assembleOutput does.
func (p *outputParser) finalizeBullet() {
	if p.current == nil || p.bullet == "" {
		return
	}
	p.current.Bullets = append(p.current.Bullets, strings.TrimRight(p.bullet, "\n \t"))
	p.bullet = ""
}

// finalizeSection finalizes the open bullet and section.
func (p *outputParser) finalizeSection() {
	p.finalizeBullet()
	if p.current == nil {
		return
	}
	p.sections = append(p.sections, *p.current)
	p.current = nil
}

// attributeSections zips each section's bullets positionally onto its enabled
// rules in emission order. It is all-or-nothing.
func attributeSections(
	ctx context.Context,
	allRules, enabled []Rule,
	sections []outputSection,
) (map[string]string, error) {
	known := make(map[string]struct{}, len(allRules))
	for _, r := range allRules {
		known[r.Section] = struct{}{}
	}
	groups, _ := groupBySection(enabled)

	bulletsBySection := make(map[string][]string, len(sections))
	for _, s := range sections {
		if _, ok := known[s.Name]; !ok {
			return nil, errors.Errorf(
				ctx,
				"output section %q does not exist in the rules",
				s.Name,
			)
		}
		bulletsBySection[s.Name] = s.Bullets
	}

	bulletByID := make(map[string]string, len(enabled))
	for name, rules := range groups {
		bullets, ok := bulletsBySection[name]
		if !ok {
			return nil, errors.Errorf(ctx, "output is missing section %q", name)
		}
		if len(bullets) != len(rules) {
			return nil, errors.Errorf(
				ctx,
				"section %q has %d bullets but %d enabled rules",
				name,
				len(bullets),
				len(rules),
			)
		}
		for i, r := range rules {
			bulletByID[r.ID] = bullets[i]
		}
	}

	for _, s := range sections {
		if _, ok := groups[s.Name]; ok {
			continue
		}
		if len(s.Bullets) != 0 {
			return nil, errors.Errorf(
				ctx,
				"section %q has bullets but no enabled rules",
				s.Name,
			)
		}
	}
	return bulletByID, nil
}

// rebuildFromOutput reads outputPath and returns a bullet for every enabled
// rule id, or an error when the output cannot be attributed exactly.
func rebuildFromOutput(
	ctx context.Context,
	outputPath string,
	allRules, enabled []Rule,
) (map[string]string, error) {
	// #nosec G304 -- outputPath is the operator-supplied --output argument, not free-form input
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "read committed output %q", outputPath)
	}
	sections, err := parseOutputSections(ctx, string(data))
	if err != nil {
		return nil, err
	}
	return attributeSections(ctx, allRules, enabled, sections)
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

	previousByID := d.previousBullets(misses)

	batchSize := d.BatchSize
	if batchSize <= 0 {
		batchSize = 15
	}

	failingIDs, _, compiledCount, chunkCount, err := d.compileChunks(
		ctx,
		misses,
		previousByID,
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
		// Retry one rule per prompt. Re-batching the failures at batchSize
		// reproduces the same multi-bullet emission that failed validation, so
		// the retry would fail the same way and abort the whole run.
		stillFailing, stillMissing, _, _, retryErr := d.compileChunks(
			ctx,
			retryRules,
			previousByID,
			bulletByID,
			1,
		)
		if retryErr != nil {
			return nil, retryErr
		}
		// A MISSING bullet means the model returned no delimiter for that id —
		// the hijack/refusal path, where writing the output would be unsafe.
		// Keep that fatal. A bullet that came back but failed validation is a
		// compression-shape problem in one rule file; warn and carry on so two
		// broken rules cannot block regeneration of the whole corpus.
		if len(stillMissing) > 0 {
			return nil, errors.Errorf(ctx, "unresolved rule ids after retry: %v", stillMissing)
		}
		d.keepOrSkip(stillFailing, previousByID, bulletByID)
	}

	fmt.Fprintf(d.Stderr, "distill: %d cached, %d compiled (%d chunks), %d retried\n",
		cachedCount, compiledCount, chunkCount, retriedCount)

	return bulletByID, nil
}

// previousBullets returns each missed rule's last validated bullet keyed by id.
// A miss means the rule body changed, but the cache still holds that rule's
// prior bullet; passing it lets the model revise the previous rendering instead
// of re-deriving the whole block, so a one-sentence edit does not reword every
// line. Returns an empty map under --no-cache.
func (d *Driver) previousBullets(misses []Rule) map[string]string {
	previousByID := make(map[string]string, len(misses))
	if d.NoCache {
		return previousByID
	}
	for _, r := range misses {
		if prev, ok := d.Cache.GetPrevious(r.ID); ok {
			previousByID[r.ID] = prev
		}
	}
	return previousByID
}

// compileChunks sends rules in batches of batchSize to the runner, validates
// each returned bullet, and accumulates results into bulletByID. It returns
// the ids that failed to produce a valid bullet, plus counts.
func (d *Driver) compileChunks(
	ctx context.Context,
	rules []Rule,
	previousByID map[string]string,
	bulletByID map[string]string,
	batchSize int,
) (failingIDs []string, missingIDs []string, compiled int, chunks int, err error) {
	for i := 0; i < len(rules); i += batchSize {
		// Non-blocking context cancellation check between chunks.
		select {
		case <-ctx.Done():
			return nil, nil, 0, 0, errors.Wrapf(ctx, ctx.Err(), "cancelled")
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
			bodies = append(bodies, RuleBody{
				ID:       r.ID,
				Body:     r.Body,
				Previous: previousByID[r.ID],
			})
			chunkIDs = append(chunkIDs, r.ID)
		}

		prompt, buildErr := BuildBatchPrompt(ctx, bodies)
		if buildErr != nil {
			return nil, nil, 0, chunks, buildErr
		}

		resp, runErr := d.Runner.Run(ctx, d.Model, SystemPrompt(), prompt)
		if runErr != nil {
			return nil, nil, 0, chunks, errors.Wrapf(
				ctx,
				runErr,
				"claude run chunk ids=%v",
				chunkIDs,
			)
		}

		bullets, warnings, _ := ParseBatchResponse(resp, chunkIDs)
		for _, w := range warnings {
			fmt.Fprintf(d.Stderr, "distill: warning: %s\n", w)
		}

		chunkFailing, chunkMissing, chunkCompiled := d.processChunkResults(
			ctx, chunk, bullets, bulletByID,
		)
		failingIDs = append(failingIDs, chunkFailing...)
		missingIDs = append(missingIDs, chunkMissing...)
		compiled += chunkCompiled
	}
	return failingIDs, missingIDs, compiled, chunks, nil
}

// keepOrSkip handles rules whose bullet came back malformed on both attempts.
// A rule with a cached previous rendering keeps it; one without is left out of
// the output. Either way the id is named on stderr.
func (d *Driver) keepOrSkip(ids []string, previousByID, bulletByID map[string]string) {
	for _, id := range ids {
		if prev, ok := previousByID[id]; ok {
			bulletByID[id] = prev
			fmt.Fprintf(
				d.Stderr,
				"distill: warning: rule id=%q failed validation after retry — kept its previous rendering\n",
				id,
			)
			continue
		}
		fmt.Fprintf(
			d.Stderr,
			"distill: warning: rule id=%q failed validation after retry — omitted from the output\n",
			id,
		)
	}
}

// processChunkResults validates each bullet returned for a chunk and records
// valid ones into bulletByID. It returns the failing ids and the compiled count.
func (d *Driver) processChunkResults(
	ctx context.Context,
	chunk []Rule,
	bullets map[string]string,
	bulletByID map[string]string,
) (failingIDs []string, missingIDs []string, compiled int) {
	for _, r := range chunk {
		bullet, found := bullets[r.ID]
		if !found {
			failingIDs = append(failingIDs, r.ID)
			missingIDs = append(missingIDs, r.ID)
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
