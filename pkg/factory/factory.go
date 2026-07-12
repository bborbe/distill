// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package factory wires distill's collaborators into a ready-to-run Driver.
//
// This is the only place that imports concrete implementations of every
// interface — keeping the wiring centralized prevents the rest of the project
// from depending on every other package transitively.
package factory

import (
	"io"

	"github.com/bborbe/distill/pkg/distill"
)

// CreateDriver returns a *distill.Driver wired with the real Parser, Runner,
// and Writer implementations. Cache is set by pkg/cli (prompt 4).
func CreateDriver(stderr io.Writer, model, title string, verbose bool) *distill.Driver {
	return &distill.Driver{
		Parser:    distill.NewParser(),
		Runner:    distill.NewRunner(),
		Writer:    distill.NewWriter(),
		Stderr:    stderr,
		Verbose:   verbose,
		Model:     model,
		Title:     title,
		BatchSize: 15,
	}
}
