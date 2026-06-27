// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package distill compiles a folder of long-form per-rule markdown files into
// short, AI-targeted bullets written between fenced markers in one or more
// target files.
//
// Distill bundles the rules per (target, section) group into a single prompt,
// invokes `claude --print` once per group, and writes the returned bullet list
// verbatim between the section's markers. Operator prose outside the markers
// is preserved byte-for-byte.
//
// The collaborators (Parser, Resolver, Scanner, Runner, Writer) are
// interfaces so consumers and tests can swap implementations. Wiring is the
// job of `pkg/factory`.
package distill
