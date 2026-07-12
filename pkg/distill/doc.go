// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package distill compiles a flat folder of long-form per-rule markdown files
// into a single regenerated AI-targeted output file.
//
// Cache-miss rules are batched and sent to `claude --print` with system
// instructions out-of-band via `--system-prompt`; the user prompt contains only
// inert data fenced as `<rule id="…">` elements. Each returned bullet is
// validated per-id and assembled by Go under `## section` headings; the model
// never contributes output structure. A content-hash cache at
// `<source-dir>/.distill-cache.json` skips unchanged rules so re-running on
// unchanged sources spawns zero child processes.
//
// The collaborators (Parser, Runner, Writer, Cache) are interfaces so consumers
// and tests can swap implementations. Wiring is the job of `pkg/factory`.
package distill
