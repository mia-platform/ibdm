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

func TestParseWorkflowRunEvent(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		body       string
		wantAction string
		wantRun    bool
		wantErr    bool
	}{
		"valid payload": {
			body:       `{"action":"completed","workflow_run":{"id":1,"name":"Build","status":"completed"}}`,
			wantAction: "completed",
			wantRun:    true,
		},
		"invalid JSON": {
			body:    `not json`,
			wantErr: true,
		},
		"missing action field": {
			body:    `{"workflow_run":{"id":1}}`,
			wantErr: true,
		},
		"empty action field": {
			body:    `{"action":"","workflow_run":{"id":1}}`,
			wantErr: true,
		},
		"missing workflow_run field": {
			body:    `{"action":"completed"}`,
			wantErr: true,
		},
		"workflow_run field wrong type": {
			body:    `{"action":"completed","workflow_run":"not-an-object"}`,
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			action, run, err := parseWorkflowRunEvent([]byte(tc.body))
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantAction, action)
			if tc.wantRun {
				require.NotNil(t, run)
			}
		})
	}
}

func TestWorkflowRunProcessor(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	processor := &workflowRunProcessor{}

	testCases := map[string]struct {
		typesToStream map[string]source.Extra
		body          string
		expectedData  []source.Data
		expectErr     bool
	}{
		"requested action returns upsert": {
			typesToStream: map[string]source.Extra{workflowRunType: {}},
			body:          `{"action":"requested","workflow_run":{"id":1,"name":"Build","status":"queued"}}`,
			expectedData: []source.Data{
				{
					Type:      workflowRunType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{workflowRunType: map[string]any{"id": float64(1), "name": "Build", "status": "queued"}},
					Time:      fixedTime,
				},
			},
		},
		"in_progress action returns upsert": {
			typesToStream: map[string]source.Extra{workflowRunType: {}},
			body:          `{"action":"in_progress","workflow_run":{"id":1,"name":"Build","status":"in_progress"}}`,
			expectedData: []source.Data{
				{
					Type:      workflowRunType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{workflowRunType: map[string]any{"id": float64(1), "name": "Build", "status": "in_progress"}},
					Time:      fixedTime,
				},
			},
		},
		"completed action returns upsert": {
			typesToStream: map[string]source.Extra{workflowRunType: {}},
			body:          `{"action":"completed","workflow_run":{"id":1,"name":"Build","status":"completed"}}`,
			expectedData: []source.Data{
				{
					Type:      workflowRunType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{workflowRunType: map[string]any{"id": float64(1), "name": "Build", "status": "completed"}},
					Time:      fixedTime,
				},
			},
		},
		"unknown action returns nil": {
			typesToStream: map[string]source.Extra{workflowRunType: {}},
			body:          `{"action":"unknown_action","workflow_run":{"id":1}}`,
			expectedData:  nil,
		},
		"type not in typesToStream returns nil": {
			typesToStream: map[string]source.Extra{"othertype": {}},
			body:          `{"action":"completed","workflow_run":{"id":1}}`,
			expectedData:  nil,
		},
		"malformed body returns error": {
			typesToStream: map[string]source.Extra{workflowRunType: {}},
			body:          `not json`,
			expectErr:     true,
		},
		"missing workflow_run returns error": {
			typesToStream: map[string]source.Extra{workflowRunType: {}},
			body:          `{"action":"completed"}`,
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
