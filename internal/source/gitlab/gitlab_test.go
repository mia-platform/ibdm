// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdatedAtOrNow(t *testing.T) {
	testCases := map[string]struct {
		item      map[string]any
		checkTime func(t *testing.T, got time.Time)
	}{
		"valid updated_at": {
			item: map[string]any{"updated_at": "2023-11-01T00:00:00Z"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				expected, _ := time.Parse(time.RFC3339, "2023-11-01T00:00:00Z")
				assert.Equal(t, expected.UTC(), got.UTC())
			},
		},
		"invalid updated_at falls back to now": {
			item: map[string]any{"updated_at": "garbage"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
		"missing updated_at falls back to now": {
			item: map[string]any{"name": "project"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			tc.checkTime(t, updatedAtOrNow(tc.item))
		})
	}
}

func TestProjectIDFromItem(t *testing.T) {
	testCases := map[string]struct {
		item      map[string]any
		expectID  string
		expectErr bool
	}{
		"valid numeric id": {
			item:     map[string]any{"id": float64(123)},
			expectID: "123",
		},
		"large numeric id": {
			item:     map[string]any{"id": float64(9876543)},
			expectID: "9876543",
		},
		"missing id": {
			item:      map[string]any{"name": "project"},
			expectErr: true,
		},
		"string id not supported": {
			item:      map[string]any{"id": "abc"},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			id, err := getIDFromItem(tc.item)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectID, id)
		})
	}
}
