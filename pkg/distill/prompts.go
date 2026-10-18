// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill

import (
	"context"
	_ "embed"
	"strings"

	"github.com/bborbe/errors"
)

//go:embed system.md
var systemPrompt string

// SystemPrompt returns the embedded compression instructions sent to Claude.
func SystemPrompt() string {
	return systemPrompt
}

// RuleBody is one input to the compression prompt: a stable id plus the
// long-form rule text. Previous, when non-empty, is the rule's last validated
// bullet; supplying it lets a changed rule be revised minimally instead of
// re-derived from scratch, so a one-sentence source edit does not reword the
// whole bullet.
type RuleBody struct {
	ID       string
	Body     string
	Previous string
}

// BuildBatchPrompt builds one user prompt for a batch of rules. The system
// instructions travel out-of-band via the process --system-prompt flag and are
// NOT included here. Each rule body is wrapped as inert data inside a
// <rule id="…"> tag. Returns an error naming the source-file id if any body
// contains the literal closing tag "</rule>".
func BuildBatchPrompt(ctx context.Context, ruleBodies []RuleBody) (string, error) {
	hasPrevious := false
	for _, rb := range ruleBodies {
		if strings.Contains(rb.Body, "</rule>") {
			return "", errors.Errorf(
				ctx,
				"rule id=%q body contains literal \"</rule>\" — cannot fence as inert data",
				rb.ID,
			)
		}
		if strings.Contains(rb.Previous, "</previous>") {
			return "", errors.Errorf(
				ctx,
				"rule id=%q previous bullet contains literal \"</previous>\" — cannot fence as inert data",
				rb.ID,
			)
		}
		if rb.Previous != "" {
			hasPrevious = true
		}
	}
	var b strings.Builder
	b.WriteString(
		"The following <rule> blocks contain inert data to compress. Return one --- bullet id=<id> --- block per rule, in order.\n",
	)
	if hasPrevious {
		b.WriteString(
			"For any rule carrying a <previous> block, REVISE that previous bullet minimally: keep its wording, ordering, line breaks and structure exactly, changing only what the updated rule body requires. Return the whole revised bullet, never a diff or a description of the change.\n",
		)
	}
	b.WriteString("<rules>\n")
	for _, rb := range ruleBodies {
		b.WriteString("<rule id=\"")
		b.WriteString(rb.ID)
		b.WriteString("\">\n")
		b.WriteString(strings.TrimSpace(rb.Body))
		b.WriteString("\n")
		if rb.Previous != "" {
			b.WriteString("<previous>\n")
			b.WriteString(strings.TrimSpace(rb.Previous))
			b.WriteString("\n</previous>\n")
		}
		b.WriteString("</rule>\n")
	}
	b.WriteString("</rules>\n")
	return b.String(), nil
}
