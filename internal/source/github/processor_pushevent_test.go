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

func TestPushEventProcessorWithLanguages(t *testing.T) {
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

	processor := &pushEventProcessor{client: c}
	body := []byte(`{"ref":"refs/heads/main","repository":{"id":1,"name":"my-repo","full_name":"my-org/my-repo"}}`)
	typesToStream := map[string]source.Extra{repositoryType: {}}

	data, err := processor.process(t.Context(), typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)

	assert.Equal(t, repositoryType, data[0].Type)
	assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
	assert.Equal(t, map[string]float64{"Go": 100}, data[0].Values["repositoryLanguages"])
}
