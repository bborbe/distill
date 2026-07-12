// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill

import (
	"context"
	"strings"

	"github.com/bborbe/errors"
)

// ValidateBullet checks that a single compressed bullet has the required shape:
// non-empty; first non-blank line starts with "- **" and contains a closing
// "**"; exactly one column-0 "- " list line (continuation lines are indented);
// balanced ``` code fences (even count). Returns a non-nil error naming the
// violation when the bullet is malformed.
func ValidateBullet(ctx context.Context, id, bullet string) error {
	trimmed := strings.TrimSpace(bullet)
	if trimmed == "" {
		return errors.Errorf(ctx, "bullet id=%q: empty bullet", id)
	}

	// First non-blank line must start with "- **" and contain a closing "**".
	firstLine := strings.SplitN(trimmed, "\n", 2)[0]
	if !strings.HasPrefix(firstLine, "- **") || !strings.Contains(firstLine[len("- **"):], "**") {
		return errors.Errorf(ctx, "bullet id=%q: missing bold prefix (- **…**)", id)
	}

	// Count column-0 "- " list lines and ``` fence markers.
	rawLines := strings.Split(bullet, "\n")
	topLevelCount := 0
	fenceCount := 0
	for _, l := range rawLines {
		if strings.HasPrefix(l, "- ") {
			topLevelCount++
		}
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fenceCount++
		}
	}

	if topLevelCount != 1 {
		return errors.Errorf(
			ctx,
			"bullet id=%q: expected exactly 1 top-level list item, found %d",
			id,
			topLevelCount,
		)
	}
	if fenceCount%2 != 0 {
		return errors.Errorf(ctx, "bullet id=%q: unbalanced code fences", id)
	}

	return nil
}
