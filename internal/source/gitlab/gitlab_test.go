// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSource(t *testing.T) {
	testCases := map[string]struct {
		setEnv    func(t *testing.T)
		expectErr error
	}{
		"valid configuration": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_TOKEN", "my-token")
				t.Setenv("GITLAB_WEBHOOK_TOKEN", "webhook-secret")
			},
		},
		"source config error wraps ErrSourceCreation": {
			setEnv:    func(t *testing.T) { t.Helper() }, // GITLAB_TOKEN missing
			expectErr: ErrSourceCreation,
		},
		"webhook config error wraps ErrSourceCreation": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_TOKEN", "my-token")
				t.Setenv("GITLAB_WEBHOOK_PATH", "no-leading-slash")
			},
			expectErr: ErrSourceCreation,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			tc.setEnv(t)

			src, err := NewSource()
			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
				assert.Nil(t, src)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, src)
		})
	}
}

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

func TestPipelineTimeOrNow(t *testing.T) {
	testCases := map[string]struct {
		item      map[string]any
		checkTime func(t *testing.T, got time.Time)
	}{
		"valid finished_at": {
			item: map[string]any{"finished_at": "2024-03-01T10:00:00Z"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				expected, _ := time.Parse(time.RFC3339, "2024-03-01T10:00:00Z")
				assert.Equal(t, expected.UTC(), got.UTC())
			},
		},
		"invalid finished_at falls back to now": {
			item: map[string]any{"finished_at": "garbage", "created_at": "2024-02-01T08:00:00Z"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
		"missing finished_at falls back to created_at": {
			item: map[string]any{"created_at": "2024-02-01T08:00:00Z"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				expected, _ := time.Parse(time.RFC3339, "2024-02-01T08:00:00Z")
				assert.Equal(t, expected.UTC(), got.UTC())
			},
		},
		"invalid finished_at and invalid created_at falls back to now": {
			item: map[string]any{"finished_at": "bad", "created_at": "also-bad"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
		"missing both falls back to now": {
			item: map[string]any{"status": "success"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			tc.checkTime(t, pipelineTimeOrNow(tc.item))
		})
	}
}

func TestAccessTokenTimeOrNow(t *testing.T) {
	testCases := map[string]struct {
		item      map[string]any
		checkTime func(t *testing.T, got time.Time)
	}{
		"valid created_at": {
			item: map[string]any{"created_at": "2024-01-15T09:30:00Z"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				expected, _ := time.Parse(time.RFC3339, "2024-01-15T09:30:00Z")
				assert.Equal(t, expected.UTC(), got.UTC())
			},
		},
		"invalid created_at falls back to now": {
			item: map[string]any{"created_at": "not-a-date"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
		"missing created_at falls back to now": {
			item: map[string]any{"name": "my-token"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			tc.checkTime(t, accessTokenTimeOrNow(tc.item))
		})
	}
}
