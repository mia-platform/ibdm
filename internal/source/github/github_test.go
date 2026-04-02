// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestNewSource(t *testing.T) {
	testCases := map[string]struct {
		envVars   map[string]string
		expectErr error
	}{
		"valid configuration": {
			envVars: map[string]string{
				"GITHUB_TOKEN": "ghp_test123",
				"GITHUB_ORG":   "mia-platform",
			},
		},
		"missing token": {
			envVars: map[string]string{
				"GITHUB_ORG": "mia-platform",
			},
			expectErr: ErrGitHubSource,
		},
		"missing org": {
			envVars: map[string]string{
				"GITHUB_TOKEN": "ghp_test123",
			},
			expectErr: ErrGitHubSource,
		},
		"invalid page size": {
			envVars: map[string]string{
				"GITHUB_TOKEN":     "ghp_test123",
				"GITHUB_ORG":       "mia-platform",
				"GITHUB_PAGE_SIZE": "0",
			},
			expectErr: ErrGitHubSource,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			s, err := NewSource()
			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
				assert.Nil(t, s)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, s)
		})
	}
}

func TestStartSyncProcess(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	testCases := map[string]struct {
		typesToSync  map[string]source.Extra
		handler      http.HandlerFunc
		expectedData []source.Data
		expectErr    error
	}{
		"single repository type with data": {
			typesToSync: map[string]source.Extra{
				repositoryType: {"apiVersion": "2026-03-10"},
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]map[string]any{
					{"id": float64(1), "name": "repo1"},
					{"id": float64(2), "name": "repo2"},
				})
			},
			expectedData: []source.Data{
				{
					Type:      repositoryType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{repositoryType: map[string]any{"id": float64(1), "name": "repo1"}},
					Time:      fixedTime,
				},
				{
					Type:      repositoryType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{repositoryType: map[string]any{"id": float64(2), "name": "repo2"}},
					Time:      fixedTime,
				},
			},
		},
		"unknown type is skipped": {
			typesToSync: map[string]source.Extra{
				"unknowntype": {},
			},
			handler: func(_ http.ResponseWriter, _ *http.Request) {
				t.Fatal("no API request should be made for unknown types")
			},
			expectedData: nil,
		},
		"mixed known and unknown types": {
			typesToSync: map[string]source.Extra{
				repositoryType: {"apiVersion": "2026-03-10"},
				"unknowntype":  {},
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]map[string]any{
					{"id": float64(1), "name": "repo1"},
				})
			},
			expectedData: []source.Data{
				{
					Type:      repositoryType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{repositoryType: map[string]any{"id": float64(1), "name": "repo1"}},
					Time:      fixedTime,
				},
			},
		},
		"empty API response pushes no data": {
			typesToSync: map[string]source.Extra{
				repositoryType: {"apiVersion": "2026-03-10"},
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]map[string]any{})
			},
			expectedData: nil,
		},
		"API error returns wrapped error": {
			typesToSync: map[string]source.Extra{
				repositoryType: {"apiVersion": "2026-03-10"},
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"message":"error"}`))
			},
			expectedData: nil,
			expectErr:    ErrGitHubSource,
		},
		"repository with full_name fetches languages": {
			typesToSync: map[string]source.Extra{
				repositoryType: {"apiVersion": "2026-03-10"},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/orgs/test-org/repos":
					json.NewEncoder(w).Encode([]map[string]any{
						{"id": float64(1), "name": "repo1", "full_name": "test-org/repo1"},
					})
				case "/repos/test-org/repo1/languages":
					json.NewEncoder(w).Encode(map[string]float64{"Go": 100000})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			expectedData: []source.Data{
				{
					Type:      repositoryType,
					Operation: source.DataOperationUpsert,
					Values: map[string]any{
						repositoryType:        map[string]any{"id": float64(1), "name": "repo1", "full_name": "test-org/repo1"},
						"repositoryLanguages": map[string]float64{"Go": 100},
					},
					Time: fixedTime,
				},
			},
		},
		"languages API error is silently skipped": {
			typesToSync: map[string]source.Extra{
				repositoryType: {"apiVersion": "2026-03-10"},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/orgs/test-org/repos":
					json.NewEncoder(w).Encode([]map[string]any{
						{"id": float64(1), "name": "repo1", "full_name": "test-org/repo1"},
					})
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			expectedData: []source.Data{
				{
					Type:      repositoryType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{repositoryType: map[string]any{"id": float64(1), "name": "repo1", "full_name": "test-org/repo1"}},
					Time:      fixedTime,
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			t.Cleanup(server.Close)

			s := &Source{
				config: config{
					URL:   server.URL,
					Org:   "test-org",
					Token: "test-token",
				},
				client: &client{
					baseURL:    server.URL,
					org:        "test-org",
					token:      "test-token",
					pageSize:   100,
					httpClient: server.Client(),
				},
			}

			results := make(chan source.Data, 100)

			err := s.StartSyncProcess(t.Context(), tc.typesToSync, results)
			close(results)

			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
				return
			}

			require.NoError(t, err)

			var got []source.Data
			for d := range results {
				got = append(got, d)
			}

			assert.Equal(t, tc.expectedData, got)
		})
	}
}

func TestStartSyncProcessConcurrencyGuard(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	t.Cleanup(server.Close)

	s := &Source{
		client: &client{
			baseURL:    server.URL,
			org:        "test-org",
			token:      "test-token",
			pageSize:   100,
			httpClient: server.Client(),
		},
	}

	// Lock the mutex to simulate a running sync
	s.syncLock.Lock()

	results := make(chan source.Data, 100)
	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{
		repositoryType: {},
	}, results)

	// Should return nil immediately without error
	require.NoError(t, err)

	s.syncLock.Unlock()
}

func TestStartSyncProcessContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", `<http://example.com?page=2>; rel="next"`)
		json.NewEncoder(w).Encode([]map[string]any{{"id": float64(1)}})
	}))
	t.Cleanup(server.Close)

	s := &Source{
		client: &client{
			baseURL:    server.URL,
			org:        "test-org",
			token:      "test-token",
			pageSize:   100,
			httpClient: server.Client(),
		},
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	results := make(chan source.Data, 100)
	err := s.StartSyncProcess(ctx, map[string]source.Extra{
		repositoryType: {"apiVersion": "2026-03-10"},
	}, results)

	// Context cancellation returns nil
	require.NoError(t, err)
}

func TestApiVersionFromExtra(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		extra    source.Extra
		expected string
	}{
		"explicit version": {
			extra:    source.Extra{"apiVersion": "2024-01-01"},
			expected: "2024-01-01",
		},
		"absent key uses default": {
			extra:    source.Extra{},
			expected: defaultAPIVersion,
		},
		"empty string uses default": {
			extra:    source.Extra{"apiVersion": ""},
			expected: defaultAPIVersion,
		},
		"nil extra uses default": {
			extra:    nil,
			expected: defaultAPIVersion,
		},
		"non-string value uses default": {
			extra:    source.Extra{"apiVersion": 123},
			expected: defaultAPIVersion,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, apiVersionFromExtra(tc.extra))
		})
	}
}

func TestExtractOwnerRepo(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		repo      map[string]any
		wantOwner string
		wantName  string
	}{
		"valid repo": {
			repo:      map[string]any{"name": "my-repo", "owner": map[string]any{"login": "org-name"}},
			wantOwner: "org-name",
			wantName:  "my-repo",
		},
		"missing owner field": {
			repo:      map[string]any{"name": "my-repo"},
			wantOwner: "",
			wantName:  "my-repo",
		},
		"owner is not an object": {
			repo:      map[string]any{"name": "my-repo", "owner": "string"},
			wantOwner: "",
			wantName:  "my-repo",
		},
		"missing name field": {
			repo:      map[string]any{"owner": map[string]any{"login": "org"}},
			wantOwner: "org",
			wantName:  "",
		},
		"missing login in owner": {
			repo:      map[string]any{"name": "repo", "owner": map[string]any{}},
			wantOwner: "",
			wantName:  "repo",
		},
		"empty map": {
			repo:      map[string]any{},
			wantOwner: "",
			wantName:  "",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			owner, repoName := extractOwnerRepo(tc.repo)
			assert.Equal(t, tc.wantOwner, owner)
			assert.Equal(t, tc.wantName, repoName)
		})
	}
}

func TestSyncRepositoryWorkflowRuns(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	testCases := map[string]struct {
		repo         map[string]any
		handler      http.HandlerFunc
		expectedData []source.Data
		expectErr    error
	}{
		"happy path with runs": {
			repo: map[string]any{"name": "repo1", "owner": map[string]any{"login": "test-org"}},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"total_count":   2,
					"workflow_runs": []map[string]any{{"id": float64(10), "name": "Build"}, {"id": float64(11), "name": "Test"}},
				})
			},
			expectedData: []source.Data{
				{
					Type:      workflowRunType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{workflowRunType: map[string]any{"id": float64(10), "name": "Build"}},
					Time:      fixedTime,
				},
				{
					Type:      workflowRunType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{workflowRunType: map[string]any{"id": float64(11), "name": "Test"}},
					Time:      fixedTime,
				},
			},
		},
		"empty runs returns no data": {
			repo: map[string]any{"name": "repo1", "owner": map[string]any{"login": "test-org"}},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"total_count": 0, "workflow_runs": []map[string]any{}})
			},
			expectedData: nil,
		},
		"API error on runs returns error": {
			repo: map[string]any{"name": "repo1", "owner": map[string]any{"login": "test-org"}},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectErr: ErrRetrievingAssets,
		},
		"missing owner skips silently": {
			repo: map[string]any{"name": "repo1"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				t.Fatal("no API call should be made when owner is missing")
			},
			expectedData: nil,
		},
		"missing name skips silently": {
			repo: map[string]any{"owner": map[string]any{"login": "test-org"}},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				t.Fatal("no API call should be made when name is missing")
			},
			expectedData: nil,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			t.Cleanup(server.Close)

			s := &Source{
				client: &client{
					baseURL:    server.URL,
					org:        "test-org",
					token:      "test-token",
					pageSize:   100,
					httpClient: server.Client(),
				},
			}

			results := make(chan source.Data, 100)
			err := s.syncRepositoryWorkflowRuns(t.Context(), tc.repo, "2026-03-10", results)
			close(results)

			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
				return
			}

			require.NoError(t, err)

			var got []source.Data
			for d := range results {
				got = append(got, d)
			}

			assert.Equal(t, tc.expectedData, got)
		})
	}
}

func TestSyncRepositoryWorkflowRunsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", `<http://example.com?page=2>; rel="next"`)
		json.NewEncoder(w).Encode(map[string]any{
			"total_count":   1,
			"workflow_runs": []map[string]any{{"id": float64(1)}},
		})
	}))
	t.Cleanup(server.Close)

	s := &Source{
		client: &client{
			baseURL:    server.URL,
			org:        "test-org",
			token:      "test-token",
			pageSize:   100,
			httpClient: server.Client(),
		},
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	repo := map[string]any{"name": "repo1", "owner": map[string]any{"login": "test-org"}}
	results := make(chan source.Data, 100)
	err := s.syncRepositoryWorkflowRuns(ctx, repo, "2026-03-10", results)

	require.ErrorIs(t, err, context.Canceled)
}

func TestStartSyncProcessWithWorkflowRunType(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	testCases := map[string]struct {
		typesToSync  map[string]source.Extra
		handler      http.HandlerFunc
		expectedData []source.Data
		expectErr    error
	}{
		"workflow_run only": {
			typesToSync: map[string]source.Extra{
				workflowRunType: {"apiVersion": "2026-03-10"},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/orgs/test-org/repos":
					json.NewEncoder(w).Encode([]map[string]any{
						{"name": "repo1", "owner": map[string]any{"login": "test-org"}},
					})
				case "/repos/test-org/repo1/actions/runs":
					json.NewEncoder(w).Encode(map[string]any{
						"total_count":   1,
						"workflow_runs": []map[string]any{{"id": float64(10), "name": "Build"}},
					})
				}
			},
			expectedData: []source.Data{
				{
					Type:      workflowRunType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{workflowRunType: map[string]any{"id": float64(10), "name": "Build"}},
					Time:      fixedTime,
				},
			},
		},
		"both repository and workflow_run types": {
			typesToSync: map[string]source.Extra{
				repositoryType:  {"apiVersion": "2026-03-10"},
				workflowRunType: {"apiVersion": "2026-03-10"},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/orgs/test-org/repos":
					json.NewEncoder(w).Encode([]map[string]any{
						{"id": float64(1), "name": "repo1", "owner": map[string]any{"login": "test-org"}},
					})
				case "/repos/test-org/repo1/actions/runs":
					json.NewEncoder(w).Encode(map[string]any{
						"total_count":   1,
						"workflow_runs": []map[string]any{{"id": float64(10), "name": "Build"}},
					})
				}
			},
			expectedData: []source.Data{
				{
					Type:      repositoryType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{repositoryType: map[string]any{"id": float64(1), "name": "repo1", "owner": map[string]any{"login": "test-org"}}},
					Time:      fixedTime,
				},
				{
					Type:      workflowRunType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{workflowRunType: map[string]any{"id": float64(10), "name": "Build"}},
					Time:      fixedTime,
				},
			},
		},
		"unknown type alongside workflow_run": {
			typesToSync: map[string]source.Extra{
				workflowRunType: {"apiVersion": "2026-03-10"},
				"unknowntype":   {},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/orgs/test-org/repos":
					json.NewEncoder(w).Encode([]map[string]any{
						{"name": "repo1", "owner": map[string]any{"login": "test-org"}},
					})
				case "/repos/test-org/repo1/actions/runs":
					json.NewEncoder(w).Encode(map[string]any{
						"total_count":   1,
						"workflow_runs": []map[string]any{{"id": float64(10)}},
					})
				}
			},
			expectedData: []source.Data{
				{
					Type:      workflowRunType,
					Operation: source.DataOperationUpsert,
					Values:    map[string]any{workflowRunType: map[string]any{"id": float64(10)}},
					Time:      fixedTime,
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			t.Cleanup(server.Close)

			s := &Source{
				config: config{
					URL:   server.URL,
					Org:   "test-org",
					Token: "test-token",
				},
				client: &client{
					baseURL:    server.URL,
					org:        "test-org",
					token:      "test-token",
					pageSize:   100,
					httpClient: server.Client(),
				},
			}

			results := make(chan source.Data, 100)
			err := s.StartSyncProcess(t.Context(), tc.typesToSync, results)
			close(results)

			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
				return
			}

			require.NoError(t, err)

			var got []source.Data
			for d := range results {
				got = append(got, d)
			}

			assert.Equal(t, tc.expectedData, got)
		})
	}
}
