// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bborbe/errors"
	"gopkg.in/yaml.v3"
)

// Rule is one parsed source file: the frontmatter `distill:` block plus the
// markdown body with frontmatter stripped.
type Rule struct {
	// Path is the absolute path of the source file the rule was parsed from.
	Path string
	// ID is the stable identifier from `distill.id` or, when absent, the
	// filename stem (`foo.md` → `foo`).
	ID string
	// Section is the `## <section>` heading the bullet lives under in the
	// output file. Required; empty value is an error.
	Section string
	// Order is the sort key within a section; lower comes first. Defaults to
	// 100 when omitted in frontmatter.
	Order int
	// Disabled, when true, drops the rule from prompt building and emitted
	// output. The rule is still parsed so its presence is visible.
	Disabled bool
	// Body is the markdown body with YAML frontmatter stripped; sent verbatim
	// to Claude as part of the compression prompt.
	Body string
}

// distillFrontmatter is the YAML shape `distill:` blocks unmarshal into.
type distillFrontmatter struct {
	Distill *struct {
		Section  string `yaml:"section"`
		Order    *int   `yaml:"order"`
		ID       string `yaml:"id"`
		Disabled bool   `yaml:"disabled"`
	} `yaml:"distill"`
}

//counterfeiter:generate -o ../../mocks/distill-parser.go --fake-name DistillParser . Parser

// Parser walks a source directory and returns its rules.
type Parser interface {
	Parse(ctx context.Context, sourceDir string) ([]Rule, error)
}

// NewParser returns a Parser that reads .md files flat from sourceDir
// (subfolders are not recursed in v1).
func NewParser() Parser {
	return &parser{}
}

type parser struct{}

// Parse walks the source directory once, returns rules sorted by (section,
// order, id) ascending. Files without a `distill:` block are skipped silently;
// files that fail to parse YAML or lack a required `section:` are errors.
func (p *parser) Parse(ctx context.Context, sourceDir string) ([]Rule, error) {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "read source dir %q", sourceDir)
	}
	var rules []Rule
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		path := filepath.Join(sourceDir, name)
		rule, ok, err := p.parseFile(ctx, path)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		a, b := rules[i], rules[j]
		if a.Section != b.Section {
			return a.Section < b.Section
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.ID < b.ID
	})
	return rules, nil
}

func (p *parser) parseFile(ctx context.Context, path string) (Rule, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Rule{}, false, errors.Wrapf(ctx, err, "read %q", path)
	}
	frontmatter, body, ok := splitFrontmatter(raw)
	if !ok {
		return Rule{}, false, nil
	}
	var fm distillFrontmatter
	if err := yaml.Unmarshal(frontmatter, &fm); err != nil {
		return Rule{}, false, errors.Wrapf(ctx, err, "parse frontmatter in %q", path)
	}
	if fm.Distill == nil {
		return Rule{}, false, nil
	}
	if fm.Distill.Section == "" {
		return Rule{}, false, errors.Errorf(ctx, "%s: distill.section is required", path)
	}
	id := fm.Distill.ID
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	order := 100
	if fm.Distill.Order != nil {
		order = *fm.Distill.Order
	}
	return Rule{
		Path:     path,
		ID:       id,
		Section:  fm.Distill.Section,
		Order:    order,
		Disabled: fm.Distill.Disabled,
		Body:     body,
	}, true, nil
}

// splitFrontmatter returns (frontmatter, body, hasFrontmatter). The frontmatter
// is the bytes between the leading `---` line and the next `---` line; the body
// is everything after the closing delimiter (with leading newline trimmed).
func splitFrontmatter(raw []byte) ([]byte, string, bool) {
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") && !strings.HasPrefix(text, "---\r\n") {
		return nil, "", false
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(text, "---\n"), "---\r\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		end = strings.Index(rest, "\n---\r\n")
		if end < 0 {
			return nil, "", false
		}
	}
	fm := rest[:end]
	body := rest[end:]
	body = strings.TrimPrefix(body, "\n---\n")
	body = strings.TrimPrefix(body, "\n---\r\n")
	body = strings.TrimPrefix(body, "\n")
	return []byte(fm), body, true
}
