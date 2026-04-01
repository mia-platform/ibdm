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

func TestParseWorkflowDispatchEvent(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		body        string
		wantPayload bool
		wantErr     bool
	}{
		"valid payload": {
			body:        `{"workflow":".github/workflows/build.yml","ref":"refs/heads/main","inputs":null,"repository":{"id":1},"sender":{"login":"octocat"}}`,
			wantPayload: true,
		},
		"invalid JSON": {
			body:    `not json`,
			wantErr: true,
		},
		"missing workflow field": {
			body:    `{"ref":"refs/heads/main","repository":{"id":1}}`,
			wantErr: true,
		},
		"workflow field wrong type": {
			body:    `{"workflow":123,"ref":"refs/heads/main","repository":{"id":1}}`,
			wantErr: true,
		},
		"missing ref field": {
			body:    `{"workflow":".github/workflows/build.yml","repository":{"id":1}}`,
			wantErr: true,
		},
		"ref field wrong type": {
			body:    `{"workflow":".github/workflows/build.yml","ref":123,"repository":{"id":1}}`,
			wantErr: true,
		},
		"missing repository field": {
			body:    `{"workflow":".github/workflows/build.yml","ref":"refs/heads/main"}`,
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			payload, err := parseWorkflowDispatchEvent([]byte(tc.body))
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tc.wantPayload {
				require.NotNil(t, payload)
			}
		})
	}
}

func TestWorkflowDispatchProcessor(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	processor := &workflowDispatchProcessor{}

	testCases := map[string]struct {
		typesToStream map[string]source.Extra
		body          string
		expectedData  []source.Data
		expectErr     bool
	}{
		"workflow_dispatch returns upsert": {
			typesToStream: map[string]source.Extra{workflowDispatchType: {}},
			body:          `{"workflow":".github/workflows/build.yml","ref":"refs/heads/main","inputs":null,"repository":{"id":1},"sender":{"login":"octocat"}}`,
			expectedData: []source.Data{
				{
					Type:      workflowDispatchType,
					Operation: source.DataOperationUpsert,
					Values: map[string]any{workflowDispatchType: map[string]any{
						"workflow":   ".github/workflows/build.yml",
						"ref":        "refs/heads/main",
						"inputs":     nil,
						"repository": map[string]any{"id": float64(1)},
						"sender":     map[string]any{"login": "octocat"},
					}},
					Time: fixedTime,
				},
			},
		},
		"type not in typesToStream returns nil": {
			typesToStream: map[string]source.Extra{"othertype": {}},
			body:          `{"workflow":".github/workflows/build.yml","ref":"refs/heads/main","repository":{"id":1}}`,
			expectedData:  nil,
		},
		"malformed body returns error": {
			typesToStream: map[string]source.Extra{workflowDispatchType: {}},
			body:          `not json`,
			expectErr:     true,
		},
		"missing workflow field returns error": {
			typesToStream: map[string]source.Extra{workflowDispatchType: {}},
			body:          `{"ref":"refs/heads/main","repository":{"id":1}}`,
			expectErr:     true,
		},
		"missing repository field returns error": {
			typesToStream: map[string]source.Extra{workflowDispatchType: {}},
			body:          `{"workflow":".github/workflows/build.yml","ref":"refs/heads/main"}`,
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
