// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command distill compiles a folder of per-rule markdown files into one short
// AI-targeted markdown file by sending cache-miss rules through `claude --print`
// and assembling the returned per-rule bullets into one regenerated output file.
package main

import "github.com/bborbe/distill/pkg/cli"

func main() {
	cli.Execute()
}
