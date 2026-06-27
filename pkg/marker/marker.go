// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package marker scans target markdown files into a sequence of regions,
// distinguishing operator prose from `distill` marker blocks.
package marker

import (
	"context"
	"regexp"
	"strings"

	"github.com/bborbe/errors"
)

// Region is one slice of a parsed target file. Prose regions are preserved
// byte-for-byte; marker regions have their inner body replaced on write.
type Region struct {
	IsMarker bool
	// Prose is set for non-marker regions; the verbatim bytes.
	Prose string
	// Section is set for marker regions; the section attribute value.
	Section string
	// BeginLine is the literal "<!-- begin:distill section=\"X\" -->" line
	// (without trailing newline).
	BeginLine string
	// EndLine is the literal end marker line.
	EndLine string
}

var (
	beginRE = regexp.MustCompile(`^<!--\s*begin:distill\s+section="([^"]*)"\s*-->\s*$`)
	endRE   = regexp.MustCompile(`^<!--\s*end:distill\s+section="([^"]*)"\s*-->\s*$`)
)

//counterfeiter:generate -o ../../mocks/marker-scanner.go --fake-name Scanner . Scanner

// Scanner parses target file contents into a sequence of Regions.
type Scanner interface {
	Scan(ctx context.Context, path string, content string) ([]Region, error)
}

// NewScanner returns a Scanner.
func NewScanner() Scanner {
	return &scanner{}
}

type scanner struct{}

// Scan splits content into prose / marker regions. Returns an error naming the
// path on orphan begin/end markers or mismatched section attributes.
func (s *scanner) Scan(ctx context.Context, path string, content string) ([]Region, error) {
	lines := strings.SplitAfter(content, "\n")
	var regions []Region
	var prose strings.Builder
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimRight(line, "\r\n")
		if m := beginRE.FindStringSubmatch(trimmed); m != nil {
			if prose.Len() > 0 {
				regions = append(regions, Region{Prose: prose.String()})
				prose.Reset()
			}
			section := m[1]
			endIdx := -1
			for j := i + 1; j < len(lines); j++ {
				jTrim := strings.TrimRight(lines[j], "\r\n")
				if em := endRE.FindStringSubmatch(jTrim); em != nil {
					if em[1] != section {
						return nil, errors.Errorf(ctx,
							"%s: begin marker section=%q does not match end marker section=%q",
							path, section, em[1])
					}
					endIdx = j
					break
				}
				if beginRE.MatchString(jTrim) {
					return nil, errors.Errorf(ctx,
						"%s: orphan begin marker section=%q (another begin marker before its end)",
						path, section)
				}
			}
			if endIdx < 0 {
				return nil, errors.Errorf(ctx,
					"%s: orphan begin marker section=%q (no matching end marker)",
					path, section)
			}
			regions = append(regions, Region{
				IsMarker:  true,
				Section:   section,
				BeginLine: trimmed,
				EndLine:   strings.TrimRight(lines[endIdx], "\r\n"),
			})
			i = endIdx + 1
			continue
		}
		if m := endRE.FindStringSubmatch(trimmed); m != nil {
			return nil, errors.Errorf(ctx,
				"%s: orphan end marker section=%q (no preceding begin marker)",
				path, m[1])
		}
		prose.WriteString(line)
		i++
	}
	if prose.Len() > 0 {
		regions = append(regions, Region{Prose: prose.String()})
	}
	return regions, nil
}

// Render rebuilds a target file from regions, using compressedBySection to fill
// in marker blocks. Sections present in regions but missing from
// compressedBySection are written empty (the warning row).
func Render(regions []Region, compressedBySection map[string]string) string {
	var b strings.Builder
	for _, r := range regions {
		if !r.IsMarker {
			b.WriteString(r.Prose)
			continue
		}
		b.WriteString(r.BeginLine)
		b.WriteString("\n")
		body := compressedBySection[r.Section]
		body = strings.TrimRight(body, "\n")
		if body != "" {
			b.WriteString(body)
			b.WriteString("\n")
		}
		b.WriteString(r.EndLine)
		b.WriteString("\n")
	}
	return b.String()
}

// Sections returns the unique section names appearing as marker pairs in
// regions, in their on-disk order.
func Sections(regions []Region) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range regions {
		if r.IsMarker && !seen[r.Section] {
			seen[r.Section] = true
			out = append(out, r.Section)
		}
	}
	return out
}
