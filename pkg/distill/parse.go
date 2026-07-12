// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill

import (
	"fmt"
	"strings"
)

// ParseBatchResponse extracts per-id bullets from a model response delimited by
// "--- bullet id=<id> ---" lines. A delimiter line that appears inside a fenced
// code block (between ``` fences) is treated as literal content, not a
// delimiter. Text before the first real delimiter is tolerated and returned as
// a warning string, never an error. Returns the id->bullet map and a slice of
// human-readable warnings (stray preamble, or bullets addressed to ids not in
// requestedIDs, which are dropped).
func ParseBatchResponse(
	response string,
	requestedIDs []string,
) (map[string]string, []string, error) {
	reqSet := make(map[string]bool, len(requestedIDs))
	for _, id := range requestedIDs {
		reqSet[id] = true
	}
	p := &batchResponseParser{
		reqSet: reqSet,
		result: make(map[string]string),
	}
	for _, line := range strings.Split(response, "\n") {
		p.processLine(line)
	}
	p.flush()
	return p.result, p.warnings, nil
}

// batchResponseParser is a line-by-line state machine for ParseBatchResponse.
type batchResponseParser struct {
	reqSet             map[string]bool
	result             map[string]string
	warnings           []string
	inFence            bool
	firstDelimiterSeen bool
	currentID          string
	currentLines       []string
	preambleLines      []string
}

func (p *batchResponseParser) processLine(line string) {
	if strings.HasPrefix(strings.TrimSpace(line), "```") {
		p.inFence = !p.inFence
	}
	if !p.inFence {
		if id := parseBulletDelimiterID(line); id != "" {
			p.handleDelimiter(id)
			return
		}
	}
	if !p.firstDelimiterSeen {
		p.preambleLines = append(p.preambleLines, line)
	} else {
		p.currentLines = append(p.currentLines, line)
	}
}

func (p *batchResponseParser) handleDelimiter(id string) {
	p.flush()
	if !p.firstDelimiterSeen {
		p.emitPreambleWarning()
		p.firstDelimiterSeen = true
	}
	p.currentID = id
}

func (p *batchResponseParser) emitPreambleWarning() {
	nonBlank := 0
	for _, pl := range p.preambleLines {
		if strings.TrimSpace(pl) != "" {
			nonBlank++
		}
	}
	if nonBlank > 0 {
		p.warnings = append(
			p.warnings,
			fmt.Sprintf(
				"ignored %d line(s) of preamble before first bullet delimiter",
				len(p.preambleLines),
			),
		)
	}
}

func (p *batchResponseParser) flush() {
	if p.currentID == "" {
		return
	}
	body := strings.TrimRight(strings.Join(p.currentLines, "\n"), "\n \t")
	if p.reqSet[p.currentID] {
		p.result[p.currentID] = body
	} else {
		p.warnings = append(p.warnings, fmt.Sprintf("dropped bullet for unrequested id=%q", p.currentID))
	}
	p.currentID = ""
	p.currentLines = nil
}

// parseBulletDelimiterID returns the id captured from a line of the form
// "--- bullet id=<id> ---", or "" if the line does not match.
func parseBulletDelimiterID(line string) string {
	const prefix = "--- bullet id="
	const suffix = " ---"
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
		return ""
	}
	if len(line) <= len(prefix)+len(suffix) {
		return ""
	}
	return strings.TrimSpace(line[len(prefix) : len(line)-len(suffix)])
}
