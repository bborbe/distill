// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cli is the cobra-based entry layer for the `distill` binary.
//
// Execute owns signal handling and context cancellation; Run owns the cobra
// command tree and flag parsing. Splitting the two keeps Run testable without
// touching os.Exit or signal state.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bborbe/distill/pkg/distill"
	"github.com/bborbe/distill/pkg/factory"
)

// Execute is the main entry point for the distill binary. It wires
// signal-driven context cancellation around Run and translates any returned
// error into a non-zero exit code via distill.ExitCode.
func Execute() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "distill: %v\n", err)
		os.Exit(distill.ExitCode(err))
	}
}

// Run parses args, wires the driver, and invokes the pipeline. Returns nil on
// success or an error suitable for distill.ExitCode mapping.
func Run(ctx context.Context, args []string) error {
	var (
		sourceDir string
		model     string
		verbose   bool
	)

	rootCmd := &cobra.Command{
		Use:          "distill",
		Short:        "Compile a folder of per-rule markdown files into one short AI-targeted markdown file.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getwd: %w", err)
			}
			driver := factory.CreateDriver(cmd.ErrOrStderr(), model, verbose)
			return driver.Run(cmd.Context(), sourceDir, cwd)
		},
	}

	rootCmd.Flags().StringVar(&sourceDir, "source", "", "directory of source rule markdown files (required)")
	rootCmd.Flags().StringVar(&model, "model", "sonnet", "Claude model name passed to `claude --model`")
	rootCmd.Flags().BoolVar(&verbose, "verbose", false, "print per-group prompt + response to stderr")
	_ = rootCmd.MarkFlagRequired("source")

	rootCmd.SetArgs(args)
	return rootCmd.ExecuteContext(ctx)
}
