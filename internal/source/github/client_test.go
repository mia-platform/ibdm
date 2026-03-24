// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListRepositoriesFactoryURL(t *testing.T) {
	t.Parallel()

	var capturedRequest *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode([]map[string]any{{"id": 1}})
		if err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:    server.URL,
		org:        "mia-platform",
		token:      "ghp_testtoken",
		pageSize:   50,
		httpClient: server.Client(),
	}

	it := c.listRepositories("2026-03-10")
	items, err := it.next(t.Context())
	require.NoError(t, err)
	assert.Len(t, items, 1)

	require.NotNil(t, capturedRequest)
	assert.Equal(t, "/orgs/mia-platform/repos", capturedRequest.URL.Path)
	assert.Equal(t, "all", capturedRequest.URL.Query().Get("type"))
	assert.Equal(t, "50", capturedRequest.URL.Query().Get("per_page"))
	assert.Equal(t, "1", capturedRequest.URL.Query().Get("page"))
}

func TestClientRequestHeaders(t *testing.T) {
	t.Parallel()

	var capturedRequest *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode([]map[string]any{})
		if err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:    server.URL,
		org:        "test-org",
		token:      "ghp_abc123",
		pageSize:   100,
		httpClient: server.Client(),
	}

	resp, err := c.doRequest(t.Context(), "/test/path", "2026-03-10", 1)
	require.NoError(t, err)
	resp.Body.Close()

	require.NotNil(t, capturedRequest)
	assert.Equal(t, "Bearer ghp_abc123", capturedRequest.Header.Get("Authorization"))
	assert.Equal(t, "application/vnd.github+json", capturedRequest.Header.Get("Accept"))
	assert.Equal(t, "2026-03-10", capturedRequest.Header.Get("X-GitHub-Api-Version"))
	assert.Equal(t, userAgent, capturedRequest.Header.Get("User-Agent"))
}
