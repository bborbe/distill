// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cli

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersionFrom(t *testing.T) {
	info := func(v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Version: v}}
	}

	tests := []struct {
		name    string
		ldflag  string
		info    *debug.BuildInfo
		ok      bool
		want    string
		comment string
	}{
		{
			name:    "ldflag wins over build info",
			ldflag:  "v9.9.9",
			info:    info("v0.10.0"),
			ok:      true,
			want:    "v9.9.9",
			comment: "goreleaser and make install both set -X; it must be authoritative",
		},
		{
			name:    "falls back to the module version",
			ldflag:  "dev",
			info:    info("v0.10.0"),
			ok:      true,
			want:    "v0.10.0",
			comment: "go install applies no -X; the build info is the only source",
		},
		{
			name:    "devel build info falls through to the default",
			ldflag:  "dev",
			info:    info("(devel)"),
			ok:      true,
			want:    "dev",
			comment: "a local go build has no release version to report",
		},
		{
			name:    "empty build info version falls through",
			ldflag:  "dev",
			info:    info(""),
			ok:      true,
			want:    "dev",
			comment: "never report an empty line",
		},
		{
			name:    "no build info at all",
			ldflag:  "dev",
			info:    nil,
			ok:      false,
			want:    "dev",
			comment: "ReadBuildInfo can report ok=false",
		},
		{
			name:    "nil info with ok true is tolerated",
			ldflag:  "dev",
			info:    nil,
			ok:      true,
			want:    "dev",
			comment: "defensive: never dereference a nil BuildInfo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersionFrom(tt.ldflag, tt.info, tt.ok); got != tt.want {
				t.Errorf("resolveVersionFrom(%q, %+v, %v) = %q, want %q — %s",
					tt.ldflag, tt.info, tt.ok, got, tt.want, tt.comment)
			}
		})
	}
}
