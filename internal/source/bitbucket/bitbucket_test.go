// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func setupFixedTime(t *testing.T) time.Time {
	t.Helper()
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }
	return fixedTime
}

func TestStartSyncProcessRepositoriesOnly(t *testing.T) {
	fixedTime := setupFixedTime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/repositories/my-workspace":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"full_name": "my-workspace/repo1", "slug": "repo1"},
					{"full_name": "my-workspace/repo2", "slug": "repo2"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	s := &Source{
		workspace: "my-workspace",
		client: &client{
			baseURL:     server.URL,
			accessToken: "test-token",
			httpClient:  server.Client(),
		},
	}

	results := make(chan source.Data, 10)
	typesToSync := map[string]source.Extra{
		repositoryType: {},
	}

	err := s.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)
	close(results)

	var items []source.Data
	for d := range results {
		items = append(items, d)
	}

	require.Len(t, items, 2)
	assert.Equal(t, repositoryType, items[0].Type)
	assert.Equal(t, source.DataOperationUpsert, items[0].Operation)
	assert.Equal(t, "my-workspace/repo1", items[0].Values["repository"].(map[string]any)["full_name"])
	assert.Equal(t, fixedTime, items[0].Time)
	assert.Equal(t, "my-workspace/repo2", items[1].Values["repository"].(map[string]any)["full_name"])
}

func TestStartSyncProcessPipelinesOnly(t *testing.T) {
	fixedTime := setupFixedTime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/repositories/ws":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"full_name": "ws/repo1", "slug": "repo1"},
				},
			})
		case "/2.0/repositories/ws/repo1/pipelines":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"uuid": "{pipe-1}", "build_number": float64(1)},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	s := &Source{
		workspace: "ws",
		client: &client{
			baseURL:     server.URL,
			accessToken: "test-token",
			httpClient:  server.Client(),
		},
	}

	results := make(chan source.Data, 10)
	typesToSync := map[string]source.Extra{
		pipelineType: {},
	}

	err := s.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)
	close(results)

	var items []source.Data
	for d := range results {
		items = append(items, d)
	}

	require.Len(t, items, 1)
	assert.Equal(t, pipelineType, items[0].Type)
	assert.Equal(t, source.DataOperationUpsert, items[0].Operation)
	assert.Equal(t, "{pipe-1}", items[0].Values["pipeline"].(map[string]any)["uuid"])
	assert.Equal(t, "ws/repo1", items[0].Values["repository"].(map[string]any)["full_name"])
	assert.Equal(t, fixedTime, items[0].Time)
}

func TestStartSyncProcessBothTypes(t *testing.T) {
	setupFixedTime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/repositories/ws":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"full_name": "ws/repo1", "slug": "repo1"},
				},
			})
		case "/2.0/repositories/ws/repo1/pipelines":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"uuid": "{pipe-1}", "build_number": float64(1)},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	s := &Source{
		workspace: "ws",
		client: &client{
			baseURL:     server.URL,
			accessToken: "test-token",
			httpClient:  server.Client(),
		},
	}

	results := make(chan source.Data, 10)
	typesToSync := map[string]source.Extra{
		repositoryType: {},
		pipelineType:   {},
	}

	err := s.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)
	close(results)

	var items []source.Data
	for d := range results {
		items = append(items, d)
	}

	require.Len(t, items, 2)
	assert.Equal(t, repositoryType, items[0].Type)
	assert.Equal(t, pipelineType, items[1].Type)
}

func TestStartSyncProcessWorkspaceAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server error"}`))
	}))
	t.Cleanup(server.Close)

	s := &Source{
		client: &client{
			baseURL:     server.URL,
			accessToken: "test-token",
			httpClient:  server.Client(),
		},
	}

	results := make(chan source.Data, 10)
	typesToSync := map[string]source.Extra{
		repositoryType: {},
	}

	err := s.StartSyncProcess(t.Context(), typesToSync, results)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBitbucketSource)
}

func TestStartSyncProcessRepositoryAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/user/workspaces":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"workspace": map[string]any{"slug": "ws"}},
				},
			})
		case "/2.0/repositories/ws":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"server error"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	s := &Source{
		client: &client{
			baseURL:     server.URL,
			accessToken: "test-token",
			httpClient:  server.Client(),
		},
	}

	results := make(chan source.Data, 10)
	typesToSync := map[string]source.Extra{
		repositoryType: {},
	}

	err := s.StartSyncProcess(t.Context(), typesToSync, results)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBitbucketSource)
}

func TestStartSyncProcessPipelineAPIErrorContinues(t *testing.T) {
	setupFixedTime(t)

	var pipelineCallCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/repositories/ws":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"full_name": "ws/repo1", "slug": "repo1"},
					{"full_name": "ws/repo2", "slug": "repo2"},
				},
			})
		case "/2.0/repositories/ws/repo1/pipelines":
			pipelineCallCount.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"pipeline error"}`))
		case "/2.0/repositories/ws/repo2/pipelines":
			pipelineCallCount.Add(1)
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"uuid": "{pipe-2}", "build_number": float64(1)},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	s := &Source{
		workspace: "ws",
		client: &client{
			baseURL:     server.URL,
			accessToken: "test-token",
			httpClient:  server.Client(),
		},
	}

	results := make(chan source.Data, 10)
	typesToSync := map[string]source.Extra{
		pipelineType: {},
	}

	err := s.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)
	close(results)

	var items []source.Data
	for d := range results {
		items = append(items, d)
	}

	// repo1 pipeline fails, repo2 pipeline succeeds
	require.Len(t, items, 1)
	assert.Equal(t, pipelineType, items[0].Type)
	assert.Equal(t, int32(2), pipelineCallCount.Load())
}

func TestStartSyncProcessContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{{"slug": "ws"}},
			"next":   "http://never-called/next",
		})
	}))
	t.Cleanup(server.Close)

	s := &Source{
		client: &client{
			baseURL:     server.URL,
			accessToken: "test-token",
			httpClient:  server.Client(),
		},
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	results := make(chan source.Data, 10)
	typesToSync := map[string]source.Extra{
		repositoryType: {},
	}

	err := s.StartSyncProcess(ctx, typesToSync, results)
	require.NoError(t, err)
}

func TestStartSyncProcessSyncLockAlreadyHeld(t *testing.T) {
	s := &Source{
		client: &client{},
	}

	s.syncLock.Lock()
	defer s.syncLock.Unlock()

	results := make(chan source.Data, 10)
	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{repositoryType: {}}, results)
	require.NoError(t, err)
}

func TestStartSyncProcessUnknownTypeSkipped(t *testing.T) {
	s := &Source{
		workspace: "ws",
		client:    &client{},
	}

	results := make(chan source.Data, 10)
	typesToSync := map[string]source.Extra{
		"unknown_type": {},
	}

	err := s.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)
	close(results)

	var items []source.Data
	for d := range results {
		items = append(items, d)
	}
	assert.Empty(t, items)
}

func TestStartSyncProcessWithWorkspaceDiscovery(t *testing.T) {
	setupFixedTime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/user/workspaces":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"workspace": map[string]any{"slug": "ws1"}},
					{"workspace": map[string]any{"slug": "ws2"}},
				},
			})
		case "/2.0/repositories/ws1":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"full_name": "ws1/repo1", "slug": "repo1"},
				},
			})
		case "/2.0/repositories/ws2":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"full_name": "ws2/repo2", "slug": "repo2"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	s := &Source{
		// workspace is empty — should discover
		client: &client{
			baseURL:     server.URL,
			accessToken: "test-token",
			httpClient:  server.Client(),
		},
	}

	results := make(chan source.Data, 10)
	typesToSync := map[string]source.Extra{
		repositoryType: {},
	}

	err := s.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)
	close(results)

	var items []source.Data
	for d := range results {
		items = append(items, d)
	}

	require.Len(t, items, 2)
	assert.Equal(t, "ws1/repo1", items[0].Values["repository"].(map[string]any)["full_name"])
	assert.Equal(t, "ws2/repo2", items[1].Values["repository"].(map[string]any)["full_name"])
}

func TestExtractWorkspaceSlug(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		input    map[string]any
		expected string
	}{
		"nested workspace object": {
			input:    map[string]any{"workspace": map[string]any{"slug": "my-ws"}},
			expected: "my-ws",
		},
		"top-level slug fallback": {
			input:    map[string]any{"slug": "my-ws"},
			expected: "my-ws",
		},
		"empty map": {
			input:    map[string]any{},
			expected: "",
		},
		"workspace without slug": {
			input:    map[string]any{"workspace": map[string]any{"name": "no-slug"}},
			expected: "",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, extractWorkspaceSlug(tc.input))
		})
	}
}

func TestExtractRepoSlug(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		input    map[string]any
		expected string
	}{
		"has slug": {
			input:    map[string]any{"slug": "my-repo", "name": "My Repo"},
			expected: "my-repo",
		},
		"fallback to name": {
			input:    map[string]any{"name": "my-repo"},
			expected: "my-repo",
		},
		"empty map": {
			input:    map[string]any{},
			expected: "",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, extractRepoSlug(tc.input))
		})
	}
}

func TestPipelineTimeOrNow(t *testing.T) {
	fixedTime := setupFixedTime(t)

	testCases := map[string]struct {
		input    map[string]any
		expected time.Time
	}{
		"valid completed_on": {
			input:    map[string]any{"completed_on": "2025-01-10T10:05:30Z"},
			expected: time.Date(2025, 1, 10, 10, 5, 30, 0, time.UTC),
		},
		"fallback to created_on": {
			input:    map[string]any{"created_on": "2025-01-10T10:00:00Z"},
			expected: time.Date(2025, 1, 10, 10, 0, 0, 0, time.UTC),
		},
		"fallback to timeSource": {
			input:    map[string]any{},
			expected: fixedTime,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, pipelineTimeOrNow(tc.input))
		})
	}
}

func TestSplitFullName(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		input        string
		expectedWS   string
		expectedRepo string
	}{
		"valid full name": {
			input: "workspace/repo", expectedWS: "workspace", expectedRepo: "repo",
		},
		"no slash": {
			input: "repo-only", expectedWS: "", expectedRepo: "",
		},
		"empty string": {
			input: "", expectedWS: "", expectedRepo: "",
		},
		"multiple slashes": {
			input: "workspace/repo/extra", expectedWS: "workspace", expectedRepo: "repo/extra",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ws, repo := splitFullName(tc.input)
			assert.Equal(t, tc.expectedWS, ws)
			assert.Equal(t, tc.expectedRepo, repo)
		})
	}
}

func TestNewSource(t *testing.T) {
	t.Setenv("BITBUCKET_ACCESS_TOKEN", "bbtoken123")

	s, err := NewSource()
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, "bbtoken123", s.client.accessToken)
	assert.Equal(t, "https://api.bitbucket.org", s.client.baseURL)
	assert.Equal(t, "/bitbucket/webhook", s.webhookConfig.WebhookPath)
}

func TestNewSourceInvalidConfig(t *testing.T) {
	// No auth env vars set → validation error
	_, err := NewSource()
	require.ErrorIs(t, err, ErrBitbucketSource)
}

func TestSyncWorkspaceAssetsEmptySlugSkipped(t *testing.T) {
	setupFixedTime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/user/workspaces":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					// workspace with empty slug — should be skipped
					{"workspace": map[string]any{"name": "no-slug"}},
					// valid workspace
					{"workspace": map[string]any{"slug": "valid-ws"}},
				},
			})
		case "/2.0/repositories/valid-ws":
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"full_name": "valid-ws/repo1", "slug": "repo1"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	s := &Source{
		client: &client{
			baseURL:     server.URL,
			accessToken: "test-token",
			httpClient:  server.Client(),
		},
	}

	results := make(chan source.Data, 10)
	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{repositoryType: {}}, results)
	require.NoError(t, err)
	close(results)

	var items []source.Data
	for d := range results {
		items = append(items, d)
	}
	require.Len(t, items, 1)
	assert.Equal(t, "valid-ws/repo1", items[0].Values["repository"].(map[string]any)["full_name"])
}

func TestSyncRepositoryPipelinesEmptyRepoSlug(t *testing.T) {
	s := &Source{
		client: &client{},
	}

	results := make(chan source.Data, 10)
	// Repo map with no slug or name fields → extractRepoSlug returns "" → returns nil
	err := s.syncRepositoryPipelines(t.Context(), "ws", map[string]any{}, results)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestStartSyncProcessContextCanceledDuringRequest(t *testing.T) {
	// Use a pre-cancelled context so doRequest returns context.Canceled,
	// which travels through the iterator and syncWorkspaceAssets wrapping.
	// StartSyncProcess detects errors.Is(err, context.Canceled) and returns nil.
	cancelledCtx, cancel := context.WithCancel(t.Context())
	cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	s := &Source{
		client: &client{
			baseURL:     server.URL,
			accessToken: "test-token",
			httpClient:  server.Client(),
		},
	}

	results := make(chan source.Data, 10)
	err := s.StartSyncProcess(cancelledCtx, map[string]source.Extra{repositoryType: {}}, results)
	require.NoError(t, err)
}
