// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestParsePushEvent(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		body     string
		wantRepo bool
		wantErr  bool
	}{
		"valid payload": {
			body:     `{"ref":"refs/heads/main","repository":{"id":1,"name":"repo1"}}`,
			wantRepo: true,
		},
		"invalid JSON": {
			body:    `not json`,
			wantErr: true,
		},
		"missing repository field": {
			body:    `{"ref":"refs/heads/main"}`,
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo, err := parsePushEvent([]byte(tc.body))
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tc.wantRepo {
				require.NotNil(t, repo)
			}
		})
	}
}

func TestPushEventProcessor(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	processor := &pushEventProcessor{}

	testCases := map[string]struct {
		typesToStream map[string]source.Extra
		body          string
		expectedData  []source.Data
		expectErr     bool
	}{
		"push event returns upsert": {
			typesToStream: map[string]source.Extra{repositoryType: {}},
			body:          `{"ref":"refs/heads/main","repository":{"id":1,"name":"repo1"}}`,
			expectedData: []source.Data{
				{
					Type:      repositoryType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{repositoryType: map[string]any{"id": float64(1), "name": "repo1"}},
					Time:      fixedTime,
				},
			},
		},
		"type not in typesToStream returns nil": {
			typesToStream: map[string]source.Extra{"othertype": {}},
			body:          `{"ref":"refs/heads/main","repository":{"id":1,"name":"repo1"}}`,
			expectedData:  nil,
		},
		"malformed body returns error": {
			typesToStream: map[string]source.Extra{repositoryType: {}},
			body:          `not json`,
			expectErr:     true,
		},
		"missing repository field returns error": {
			typesToStream: map[string]source.Extra{repositoryType: {}},
			body:          `{"ref":"refs/heads/main"}`,
			expectErr:     true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			data, err := processor.process(t.Context(), tc.typesToStream, []byte(tc.body))
			if tc.expectErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expectedData, data)
		})
	}
}
