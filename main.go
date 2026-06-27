// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command distill compiles a folder of per-rule markdown files into one short
// AI-targeted markdown file by sending each (target, section) group through
// `claude --print` and writing the returned bullets between fenced markers.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/bborbe/distill/pkg/cli"
)

func main() {
	var (
		sourceDir string
		model     string
		verbose   bool
	)
	flag.StringVar(&sourceDir, "source", "", "directory of source rule markdown files (required)")
	flag.StringVar(&model, "model", "sonnet", "Claude model name passed to `claude --model`")
	flag.BoolVar(&verbose, "verbose", false, "print per-group prompt + response to stderr")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: distill --source <dir> [--model NAME] [--verbose]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if sourceDir == "" {
		flag.Usage()
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "distill: getwd: %v\n", err)
		os.Exit(1)
	}

	driver := cli.NewDriver(os.Stderr, model, verbose)
	if err := driver.Run(context.Background(), sourceDir, cwd); err != nil {
		fmt.Fprintf(os.Stderr, "distill: %v\n", err)
		os.Exit(cli.ExitCode(err))
	}
}
