// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command distill compiles a folder of per-rule markdown files into one short
// AI-targeted markdown file by sending each (target, section) group through
// `claude --print` and writing the returned bullets between fenced markers.
package main

import "github.com/bborbe/distill/pkg/cli"

func main() {
	cli.Execute()
}
