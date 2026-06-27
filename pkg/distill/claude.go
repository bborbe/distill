// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"

	"github.com/bborbe/errors"
)

//counterfeiter:generate -o ../../mocks/distill-runner.go --fake-name DistillRunner . Runner

// Runner invokes `claude --print` with prompt on stdin and returns the final
// `result` event's text.
type Runner interface {
	Run(ctx context.Context, model string, prompt string) (string, error)
}

// NewRunner returns a Runner that spawns the `claude` binary on $PATH.
func NewRunner() Runner {
	return &runner{}
}

type runner struct{}

type streamEvent struct {
	Type   string `json:"type"`
	Result string `json:"result"`
}

// Run executes `claude --print --output-format stream-json --verbose
// --strict-mcp-config --model <model>`, sends prompt on stdin, parses stdout
// stream-JSON, and returns the final `result` event's text. Trailing whitespace
// is trimmed.
func (r *runner) Run(ctx context.Context, model string, prompt string) (string, error) {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
		"--strict-mcp-config",
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = bytes.NewBufferString(prompt)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", errors.Wrap(ctx, err, "create stdout pipe")
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", errors.Wrapf(ctx, err, "start claude CLI (is `claude` on $PATH?)")
	}
	result := scanResult(stdoutPipe)
	if err := cmd.Wait(); err != nil {
		tail := tailLine(stderr.String(), 512)
		return "", errors.Wrapf(ctx, err, "claude CLI failed: %s", tail)
	}
	if result == "" {
		return "", errors.New(ctx, "claude CLI produced no result event")
	}
	return strings.TrimRight(result, " \t\r\n"), nil
}

func scanResult(reader io.Reader) string {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	var result string
	for scanner.Scan() {
		var ev streamEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type == "result" && ev.Result != "" {
			result = ev.Result
		}
	}
	_ = scanner.Err()
	return result
}

func tailLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		s = s[len(s)-max:]
	}
	return s
}
