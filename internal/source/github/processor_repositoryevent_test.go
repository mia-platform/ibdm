// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestParseRepositoryEvent(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		body       string
		wantAction string
		wantRepo   bool
		wantErr    bool
	}{
		"valid payload": {
			body:       `{"action":"created","repository":{"id":1,"name":"repo1"}}`,
			wantAction: "created",
			wantRepo:   true,
		},
		"invalid JSON": {
			body:    `not json`,
			wantErr: true,
		},
		"missing action field": {
			body:    `{"repository":{"id":1}}`,
			wantErr: true,
		},
		"missing repository field": {
			body:    `{"action":"created"}`,
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			action, repo, err := parseRepositoryEvent([]byte(tc.body))
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantAction, action)
			if tc.wantRepo {
				require.NotNil(t, repo)
			}
		})
	}
}

func TestRepositoryEventProcessor(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	processor := &repositoryEventProcessor{}

	testCases := map[string]struct {
		typesToStream map[string]source.Extra
		body          string
		expectedData  []source.Data
		expectErr     bool
	}{
		"created action returns upsert": {
			typesToStream: map[string]source.Extra{repositoryType: {}},
			body:          `{"action":"created","repository":{"id":1,"name":"repo1"}}`,
			expectedData: []source.Data{
				{
					Type:      repositoryType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{repositoryType: map[string]any{"id": float64(1), "name": "repo1"}},
					Time:      fixedTime,
				},
			},
		},
		"deleted action returns delete": {
			typesToStream: map[string]source.Extra{repositoryType: {}},
			body:          `{"action":"deleted","repository":{"id":1,"name":"repo1"}}`,
			expectedData: []source.Data{
				{
					Type:      repositoryType,
					Operation: source.DataOperationDelete,
					Values:    map[string]any{repositoryType: map[string]any{"id": float64(1), "name": "repo1"}},
					Time:      fixedTime,
				},
			},
		},
		"unknown action returns nil": {
			typesToStream: map[string]source.Extra{repositoryType: {}},
			body:          `{"action":"unknown_action","repository":{"id":1,"name":"repo1"}}`,
			expectedData:  nil,
		},
		"type not in typesToStream returns nil": {
			typesToStream: map[string]source.Extra{"othertype": {}},
			body:          `{"action":"created","repository":{"id":1,"name":"repo1"}}`,
			expectedData:  nil,
		},
		"malformed body returns error": {
			typesToStream: map[string]source.Extra{repositoryType: {}},
			body:          `not json`,
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

func TestRepositoryEventProcessorWithLanguages(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/my-org/my-repo/languages":
			json.NewEncoder(w).Encode(map[string]float64{"Go": 100000})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:    server.URL,
		org:        "my-org",
		token:      "ghp_test",
		pageSize:   50,
		httpClient: server.Client(),
	}

	processor := &repositoryEventProcessor{client: c}
	body := []byte(`{"action":"created","repository":{"id":1,"name":"my-repo","full_name":"my-org/my-repo"}}`)
	typesToStream := map[string]source.Extra{repositoryType: {}}

	data, err := processor.process(t.Context(), typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)

	assert.Equal(t, repositoryType, data[0].Type)
	assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
	assert.Equal(t, map[string]float64{"Go": 100}, data[0].Values["repositoryLanguages"])
}
