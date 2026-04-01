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

func TestListWorkflowRunsFactoryURL(t *testing.T) {
	t.Parallel()

	var capturedRequest *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(map[string]any{
			"total_count":   1,
			"workflow_runs": []map[string]any{{"id": 1, "name": "Build"}},
		})
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

	it := c.listWorkflowRuns("mia-platform", "my-repo", "2026-03-10")
	items, err := it.next(t.Context())
	require.NoError(t, err)
	assert.Len(t, items, 1)

	require.NotNil(t, capturedRequest)
	assert.Equal(t, "/repos/mia-platform/my-repo/actions/runs", capturedRequest.URL.Path)
	assert.Equal(t, "50", capturedRequest.URL.Query().Get("per_page"))
	assert.Equal(t, "1", capturedRequest.URL.Query().Get("page"))
}

func TestComputeLanguagePercentages(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		input    map[string]float64
		expected map[string]float64
	}{
		"typical sample": {
			input:    map[string]float64{"Go": 527079, "Makefile": 13129, "Dockerfile": 362},
			expected: map[string]float64{"Go": 97.5, "Makefile": 2.43, "Dockerfile": 0.07},
		},
		"single language": {
			input:    map[string]float64{"Go": 100000},
			expected: map[string]float64{"Go": 100},
		},
		"empty input": {
			input:    map[string]float64{},
			expected: map[string]float64{},
		},
		"all zero bytes": {
			input:    map[string]float64{"Go": 0},
			expected: map[string]float64{},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := computeLanguagePercentages(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetRepositoryLanguages(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		handler    func(w http.ResponseWriter, r *http.Request)
		wantErr    bool
		wantResult map[string]float64
	}{
		"happy path returns percentages": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]float64{
					"Go":         527079,
					"Makefile":   13129,
					"Dockerfile": 362,
				})
			},
			wantResult: map[string]float64{"Go": 97.5, "Makefile": 2.43, "Dockerfile": 0.07},
		},
		"non-2xx status returns error": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
		"invalid JSON returns error": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("not json"))
			},
			wantErr: true,
		},
		"empty repository returns empty percentages": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]float64{})
			},
			wantResult: map[string]float64{},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(tc.handler))
			t.Cleanup(server.Close)

			c := &client{
				baseURL:    server.URL,
				org:        "mia-platform",
				token:      "ghp_testtoken",
				pageSize:   50,
				httpClient: server.Client(),
			}

			result, err := c.getRepositoryLanguages(t.Context(), "mia-platform/my-repo", "2026-03-10")
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantResult, result)
		})
	}
}
