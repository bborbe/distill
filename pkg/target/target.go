// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package target resolves a source's `target:` value to an absolute path.
package target

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/bborbe/errors"
)

// VaultEnvVar is the environment variable read when `target: vault` is set on a
// source rule.
const VaultEnvVar = "DISTILL_VAULT_CLAUDE_MD"

// GlobalPath is the absolute path used when `target: global` is set on a source
// rule, after `~` expansion.
const GlobalPath = "~/.claude/CLAUDE.md"

//counterfeiter:generate -o ../../mocks/target-resolver.go --fake-name Resolver . Resolver

// Resolver maps a target alias or path to an absolute filesystem path.
type Resolver interface {
	Resolve(ctx context.Context, target string, cwd string) (string, error)
}

// NewResolver returns a Resolver that honours the alias table from docs/spec.md.
// It reads $DISTILL_VAULT_CLAUDE_MD via os.LookupEnv at resolve time.
func NewResolver() Resolver {
	return &resolver{}
}

type resolver struct{}

// Resolve returns the absolute path the target string refers to. Errors when
// `vault` is requested but $DISTILL_VAULT_CLAUDE_MD is unset.
func (r *resolver) Resolve(ctx context.Context, target string, cwd string) (string, error) {
	switch {
	case target == "global":
		return expandTilde(GlobalPath)
	case target == "vault":
		vault, ok := os.LookupEnv(VaultEnvVar)
		if !ok || vault == "" {
			return "", errors.Errorf(ctx, "target: vault requires $%s to be set", VaultEnvVar)
		}
		return expandTilde(vault)
	case strings.HasPrefix(target, "~"):
		return expandTilde(target)
	case filepath.IsAbs(target):
		return filepath.Clean(target), nil
	default:
		return filepath.Clean(filepath.Join(cwd, target)), nil
	}
}

func expandTilde(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return filepath.Clean(p), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if p == "~" {
		return home, nil
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Clean(filepath.Join(home, p[2:])), nil
	}
	return filepath.Clean(p), nil
}
