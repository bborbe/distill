// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cli_test

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

func TestCli(t *testing.T) {
	format.TruncatedDiff = false
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.Timeout = 60 * time.Second
	_ = time.UTC
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cli Suite", suiteConfig, reporterConfig)
}
