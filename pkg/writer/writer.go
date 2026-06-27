// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package writer atomically replaces the contents of a target file via
// temp-file + rename.
package writer

import (
	"context"
	"os"
	"path/filepath"

	"github.com/bborbe/errors"
)

//counterfeiter:generate -o ../../mocks/writer.go --fake-name Writer . Writer

// Writer writes content to path atomically.
type Writer interface {
	Write(ctx context.Context, path string, content string) error
}

// NewWriter returns a Writer that writes via temp-file + rename.
func NewWriter() Writer {
	return &writer{}
}

type writer struct{}

// Write writes content to a temp file alongside path, fsyncs it, then renames
// onto path. On failure mid-write, path is unchanged.
func (w *writer) Write(ctx context.Context, path string, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".distill-*.tmp")
	if err != nil {
		return errors.Wrapf(ctx, err, "create temp file in %q", dir)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		cleanup()
		return errors.Wrapf(ctx, err, "write temp file %q", tmpName)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return errors.Wrapf(ctx, err, "fsync temp file %q", tmpName)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return errors.Wrapf(ctx, err, "close temp file %q", tmpName)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return errors.Wrapf(ctx, err, "rename %q onto %q", tmpName, path)
	}
	return nil
}
