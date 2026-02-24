// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPipelineEvent_EventTime(t *testing.T) {
	testCases := map[string]struct {
		objectAttributes map[string]any
		checkTime        func(t *testing.T, got time.Time)
	}{
		"updated_at present and parseable": {
			objectAttributes: map[string]any{"updated_at": "2024-05-15T08:30:00Z"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				expected, _ := time.Parse(time.RFC3339, "2024-05-15T08:30:00Z")
				assert.Equal(t, expected.UTC(), got.UTC())
			},
		},
		"updated_at present but malformed falls back to now": {
			objectAttributes: map[string]any{"updated_at": "not-a-date"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
		"updated_at absent falls back to now": {
			objectAttributes: map[string]any{"status": "success"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
		"nil object_attributes falls back to now": {
			objectAttributes: nil,
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ev := &pipelineEvent{ObjectAttributes: tc.objectAttributes}
			tc.checkTime(t, ev.EventTime())
		})
	}
}
