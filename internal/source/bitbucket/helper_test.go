// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"testing"
	"time"
)

// testFixedTime is the canonical fixed time used across all time-sensitive tests.
var testFixedTime = time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

func setupFixedTime(t *testing.T) {
	t.Helper()
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return testFixedTime }
}
