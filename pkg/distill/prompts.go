// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill

import (
	_ "embed"
	"strings"
)

//go:embed system.md
var systemPrompt string

// SystemPrompt returns the embedded compression instructions sent to Claude.
func SystemPrompt() string {
	return systemPrompt
}

// Build assembles the full per-group prompt: the system instruction followed by
// each rule body delimited by a header naming its id. Bodies are emitted in the
// caller-supplied order.
func BuildPrompt(ruleBodies []RuleBody) string {
	var b strings.Builder
	b.WriteString(systemPrompt)
	b.WriteString("\n\n--- rules ---\n")
	for _, rb := range ruleBodies {
		b.WriteString("\n")
		b.WriteString("--- rule id=")
		b.WriteString(rb.ID)
		b.WriteString(" ---\n")
		b.WriteString(strings.TrimSpace(rb.Body))
		b.WriteString("\n")
	}
	return b.String()
}

// RuleBody is one input to the compression prompt: a stable id plus the
// long-form rule text.
type RuleBody struct {
	ID   string
	Body string
}
