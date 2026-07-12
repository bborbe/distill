// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distill

import "context"

//counterfeiter:generate -o ../../mocks/distill-cache.go --fake-name DistillCache . Cache

// Cache stores validated bullets keyed by rule id, guarded by a content hash so
// a changed rule (or changed compression context) is a miss.
type Cache interface {
	// Get returns the cached bullet for id when the stored hash equals hash.
	Get(id, hash string) (bullet string, ok bool)
	// Put records a validated bullet for id under hash (pending write).
	Put(id, hash, bullet string)
	// Save persists the cache, pruning to exactly keepIDs on a successful run.
	Save(ctx context.Context, keepIDs []string) error
	// SaveMerged persists all currently-Put bullets without pruning any ids.
	SaveMerged(ctx context.Context) error
	// Load reads the cache from disk; a missing/corrupt file warns and runs cold.
	Load(ctx context.Context) error
	// RuleHash derives the content hash for a rule body under the current
	// compression context (prompt-version constant + system.md + model + body).
	RuleHash(model, body string) string
}
