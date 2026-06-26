// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	var sourceDir string
	flag.StringVar(&sourceDir, "source", "", "directory of source markdown rule files")
	flag.Parse()

	if sourceDir == "" {
		fmt.Fprintln(os.Stderr, "usage: distill --source <dir>")
		os.Exit(2)
	}

	fmt.Fprintln(os.Stderr, "distill: not yet implemented — see docs/spec.md")
	os.Exit(1)
}
