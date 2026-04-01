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

func TestParsePersonalAccessTokenRequestEvent(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		body       string
		wantAction string
		wantPAT    bool
		wantErr    bool
	}{
		"valid payload": {
			body:       `{"action":"created","personal_access_token_request":{"id":1,"token_name":"my-token"}}`,
			wantAction: "created",
			wantPAT:    true,
		},
		"invalid JSON": {
			body:    `not json`,
			wantErr: true,
		},
		"missing action field": {
			body:    `{"personal_access_token_request":{"id":1}}`,
			wantErr: true,
		},
		"empty action field": {
			body:    `{"action":"","personal_access_token_request":{"id":1}}`,
			wantErr: true,
		},
		"missing personal_access_token_request field": {
			body:    `{"action":"created"}`,
			wantErr: true,
		},
		"personal_access_token_request field wrong type": {
			body:    `{"action":"created","personal_access_token_request":"not-an-object"}`,
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			action, patRequest, err := parsePersonalAccessTokenRequestEvent([]byte(tc.body))
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantAction, action)
			if tc.wantPAT {
				require.NotNil(t, patRequest)
			}
		})
	}
}

func TestPersonalAccessTokenRequestProcessor(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	processor := &personalAccessTokenRequestProcessor{}

	testCases := map[string]struct {
		typesToStream map[string]source.Extra
		body          string
		expectedData  []source.Data
		expectErr     bool
	}{
		"approved action returns upsert": {
			typesToStream: map[string]source.Extra{personalAccessTokenRequestType: {}},
			body:          `{"action":"approved","personal_access_token_request":{"id":1,"token_name":"my-token"}}`,
			expectedData: []source.Data{
				{
					Type:      personalAccessTokenRequestType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{personalAccessTokenRequestType: map[string]any{"id": float64(1), "token_name": "my-token"}},
					Time:      fixedTime,
				},
			},
		},
		"created action returns upsert": {
			typesToStream: map[string]source.Extra{personalAccessTokenRequestType: {}},
			body:          `{"action":"created","personal_access_token_request":{"id":2,"token_name":"other-token"}}`,
			expectedData: []source.Data{
				{
					Type:      personalAccessTokenRequestType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{personalAccessTokenRequestType: map[string]any{"id": float64(2), "token_name": "other-token"}},
					Time:      fixedTime,
				},
			},
		},
		"cancelled action returns delete": {
			typesToStream: map[string]source.Extra{personalAccessTokenRequestType: {}},
			body:          `{"action":"cancelled","personal_access_token_request":{"id":1,"token_name":"my-token"}}`,
			expectedData: []source.Data{
				{
					Type:      personalAccessTokenRequestType,
					Operation: source.DataOperationDelete,
					Values:    map[string]any{personalAccessTokenRequestType: map[string]any{"id": float64(1), "token_name": "my-token"}},
					Time:      fixedTime,
				},
			},
		},
		"denied action returns delete": {
			typesToStream: map[string]source.Extra{personalAccessTokenRequestType: {}},
			body:          `{"action":"denied","personal_access_token_request":{"id":1,"token_name":"my-token"}}`,
			expectedData: []source.Data{
				{
					Type:      personalAccessTokenRequestType,
					Operation: source.DataOperationDelete,
					Values:    map[string]any{personalAccessTokenRequestType: map[string]any{"id": float64(1), "token_name": "my-token"}},
					Time:      fixedTime,
				},
			},
		},
		"unknown action returns nil": {
			typesToStream: map[string]source.Extra{personalAccessTokenRequestType: {}},
			body:          `{"action":"unknown_action","personal_access_token_request":{"id":1}}`,
			expectedData:  nil,
		},
		"type not in typesToStream returns nil": {
			typesToStream: map[string]source.Extra{"othertype": {}},
			body:          `{"action":"created","personal_access_token_request":{"id":1}}`,
			expectedData:  nil,
		},
		"malformed body returns error": {
			typesToStream: map[string]source.Extra{personalAccessTokenRequestType: {}},
			body:          `not json`,
			expectErr:     true,
		},
		"missing personal_access_token_request returns error": {
			typesToStream: map[string]source.Extra{personalAccessTokenRequestType: {}},
			body:          `{"action":"created"}`,
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
