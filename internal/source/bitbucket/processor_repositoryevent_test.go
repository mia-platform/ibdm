// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

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

func TestRepositoryEventProcessorRepoPush(t *testing.T) {
	setupFixedTime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"full_name":  "ws/repo1",
			"slug":       "repo1",
			"updated_on": "2025-01-10T14:22:33Z",
		})
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:     server.URL,
		accessToken: "test-token",
		httpClient:  server.Client(),
	}

	p := &repositoryEventProcessor{}
	body := []byte(`{"repository":{"full_name":"ws/repo1","slug":"repo1"},"push":{"changes":[]}}`)

	typesToStream := map[string]source.Extra{
		repositoryType: {},
	}

	data, err := p.process(t.Context(), c, typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, repositoryType, data[0].Type)
	assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
	assert.Equal(t, "ws/repo1", data[0].Values["repository"].(map[string]any)["full_name"])
	assert.Equal(t, time.Date(2025, 1, 10, 14, 22, 33, 0, time.UTC), data[0].Time)
}

func TestRepositoryEventProcessorRepoUpdated(t *testing.T) {
	setupFixedTime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"full_name":  "ws/repo1",
			"slug":       "repo1",
			"updated_on": "2025-01-10T14:22:33Z",
		})
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:     server.URL,
		accessToken: "test-token",
		httpClient:  server.Client(),
	}

	p := &repositoryEventProcessor{}
	body := []byte(`{"repository":{"full_name":"ws/repo1","slug":"repo1"},"changes":{"name":{"new":"repo1"}}}`)

	typesToStream := map[string]source.Extra{
		repositoryType: {},
	}

	data, err := p.process(t.Context(), c, typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, repositoryType, data[0].Type)
	assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
}

func TestRepositoryEventProcessorPullRequestFulfilled(t *testing.T) {
	// TODO: this needs a review
	setupFixedTime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"full_name":  "ws/repo1",
			"slug":       "repo1",
			"updated_on": "2025-01-10T14:22:33Z",
		})
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:     server.URL,
		accessToken: "test-token",
		httpClient:  server.Client(),
	}

	p := &repositoryEventProcessor{}
	body := []byte(`{"repository":{"full_name":"ws/repo1","slug":"repo1"},"pullrequest":{"id":1}}`)

	typesToStream := map[string]source.Extra{
		repositoryType: {},
	}

	data, err := p.process(t.Context(), c, typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, repositoryType, data[0].Type)
	assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
}

func TestRepositoryEventProcessorTypeNotRequested(t *testing.T) {
	t.Parallel()

	p := &repositoryEventProcessor{}
	body := []byte(`{"repository":{"full_name":"ws/repo1"}}`)

	typesToStream := map[string]source.Extra{
		pipelineType: {},
	}

	data, err := p.process(t.Context(), nil, typesToStream, body)
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestRepositoryEventProcessorMalformedBody(t *testing.T) {
	t.Parallel()

	p := &repositoryEventProcessor{}
	typesToStream := map[string]source.Extra{
		repositoryType: {},
	}

	data, err := p.process(t.Context(), nil, typesToStream, []byte(`not json`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse webhook body")
	assert.Nil(t, data)
}

func TestRepositoryEventProcessorMissingRepositoryField(t *testing.T) {
	t.Parallel()

	p := &repositoryEventProcessor{}
	typesToStream := map[string]source.Extra{
		repositoryType: {},
	}

	data, err := p.process(t.Context(), nil, typesToStream, []byte(`{"actor":{}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'repository' field")
	assert.Nil(t, data)
}

func TestRepositoryEventProcessorEnrichmentFailure(t *testing.T) {
	fixedTime := setupFixedTime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server error"}`))
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:     server.URL,
		accessToken: "test-token",
		httpClient:  server.Client(),
	}

	p := &repositoryEventProcessor{}
	body := []byte(`{"repository":{"full_name":"ws/repo1","slug":"repo1"}}`)

	typesToStream := map[string]source.Extra{
		repositoryType: {},
	}

	data, err := p.process(t.Context(), c, typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	// Falls back to the webhook payload repository
	assert.Equal(t, "ws/repo1", data[0].Values["repository"].(map[string]any)["full_name"])
	assert.Equal(t, fixedTime, data[0].Time)
}

func TestRepositoryEventProcessorInvalidFullName(t *testing.T) {
	t.Parallel()

	p := &repositoryEventProcessor{}
	// full_name without a slash → splitFullName returns empty strings → error
	body := []byte(`{"repository":{"full_name":"noslash"}}`)
	typesToStream := map[string]source.Extra{
		repositoryType: {},
	}

	data, err := p.process(t.Context(), nil, typesToStream, body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to extract workspace/repo slug")
	assert.Nil(t, data)
}

func TestUpdatedOnOrNow(t *testing.T) {
	fixedTime := setupFixedTime(t)

	testCases := map[string]struct {
		input    map[string]any
		expected time.Time
	}{
		"valid updated_on": {
			input:    map[string]any{"updated_on": "2025-01-10T14:22:33Z"},
			expected: time.Date(2025, 1, 10, 14, 22, 33, 0, time.UTC),
		},
		"missing updated_on": {
			input:    map[string]any{},
			expected: fixedTime,
		},
		"invalid updated_on": {
			input:    map[string]any{"updated_on": "not-a-date"},
			expected: fixedTime,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, updatedOnOrNow(tc.input))
		})
	}
}
