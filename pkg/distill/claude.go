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
	"os"
	"os/exec"
	"strings"

	"github.com/bborbe/errors"
)

//counterfeiter:generate -o ../../mocks/distill-runner.go --fake-name DistillRunner . Runner

// Runner invokes `claude --print` with the compression instructions passed
// out-of-band via --system-prompt and the batch prompt on stdin. It returns the
// final `result` event's text. The child process is invoked so it cannot read
// or obey the operator's ambient CLAUDE.md.
type Runner interface {
	Run(ctx context.Context, model string, systemPrompt string, prompt string) (string, error)
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

// buildClaudeArgs returns the argv (after "claude") for the given model and
// system-prompt string.
func buildClaudeArgs(model, systemPrompt string) []string {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
		"--strict-mcp-config",
		"--system-prompt", systemPrompt,
		"--setting-sources", "",
		"--tools", "",
		"--disable-slash-commands",
		"--no-session-persistence",
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	return args
}

// neutralDir returns the neutral working directory for the claude subprocess,
// preventing the child from re-reading the project-root CLAUDE.md.
func neutralDir() string {
	return os.TempDir()
}

// Run executes `claude --print --output-format stream-json --verbose
// --strict-mcp-config --system-prompt <systemPrompt> --setting-sources ""
// --tools "" --disable-slash-commands --no-session-persistence [--model m]`,
// sends prompt on stdin, parses stdout stream-JSON, and returns the final
// `result` event's text. Trailing whitespace is trimmed.
func (r *runner) Run(
	ctx context.Context,
	model string,
	systemPrompt string,
	prompt string,
) (string, error) {
	args := buildClaudeArgs(model, systemPrompt)
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = neutralDir()
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

func tailLine(s string, maxBytes int) string {
	s = strings.TrimSpace(s)
	if len(s) > maxBytes {
		s = s[len(s)-maxBytes:]
	}
	return s
}
