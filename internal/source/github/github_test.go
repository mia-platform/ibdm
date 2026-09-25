// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

// testMappingName names the single mapping a test registers for a type, reproducing the
// one-extra-per-type configuration sources received before extras were grouped by mapping.
const testMappingName = "my-mapping"

func TestNewSource(t *testing.T) {
	testCases := map[string]struct {
		envVars   map[string]string
		expectErr error
	}{
		"valid configuration": {
			envVars: map[string]string{
				"GITHUB_TOKEN": "ghp_test123",
				"GITHUB_ORG":   "my-org",
			},
		},
		"missing token": {
			envVars: map[string]string{
				"GITHUB_ORG": "my-org",
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
				"GITHUB_ORG":       "my-org",
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
		typesToSync  map[string]source.MappingExtras
		handler      http.HandlerFunc
		expectedData []source.Data
		expectErr    error
	}{
		"single repository type with data": {
			typesToSync: map[string]source.MappingExtras{
				repositoryType: {testMappingName: {"apiVersion": "2026-03-10"}},
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
			typesToSync: map[string]source.MappingExtras{
				"unknowntype": {},
			},
			handler: func(_ http.ResponseWriter, _ *http.Request) {
				t.Fatal("no API request should be made for unknown types")
			},
			expectedData: nil,
		},
		"mixed known and unknown types": {
			typesToSync: map[string]source.MappingExtras{
				repositoryType: {testMappingName: {"apiVersion": "2026-03-10"}},
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
			typesToSync: map[string]source.MappingExtras{
				repositoryType: {testMappingName: {"apiVersion": "2026-03-10"}},
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]map[string]any{})
			},
			expectedData: nil,
		},
		"API error returns wrapped error": {
			typesToSync: map[string]source.MappingExtras{
				repositoryType: {testMappingName: {"apiVersion": "2026-03-10"}},
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"message":"error"}`))
			},
			expectedData: nil,
			expectErr:    ErrGitHubSource,
		},
		"repository with full_name fetches languages": {
			typesToSync: map[string]source.MappingExtras{
				repositoryType: {testMappingName: {"apiVersion": "2026-03-10"}},
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
			typesToSync: map[string]source.MappingExtras{
				repositoryType: {testMappingName: {"apiVersion": "2026-03-10"}},
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
	err := s.StartSyncProcess(t.Context(), map[string]source.MappingExtras{
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
	err := s.StartSyncProcess(ctx, map[string]source.MappingExtras{
		repositoryType: {testMappingName: {"apiVersion": "2026-03-10"}},
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
		"valid path with runs": {
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
			err := s.syncRepositoryWorkflowRuns(t.Context(), tc.repo, "2026-03-10", nil, results)
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
	err := s.syncRepositoryWorkflowRuns(ctx, repo, "2026-03-10", nil, results)

	require.ErrorIs(t, err, context.Canceled)
}

func TestStartSyncProcessWithWorkflowRunType(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	testCases := map[string]struct {
		typesToSync  map[string]source.MappingExtras
		handler      http.HandlerFunc
		expectedData []source.Data
		expectErr    error
	}{
		"workflow_run only": {
			typesToSync: map[string]source.MappingExtras{
				workflowRunType: {testMappingName: {"apiVersion": "2026-03-10"}},
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
			typesToSync: map[string]source.MappingExtras{
				repositoryType:  {testMappingName: {"apiVersion": "2026-03-10"}},
				workflowRunType: {testMappingName: {"apiVersion": "2026-03-10"}},
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
			typesToSync: map[string]source.MappingExtras{
				workflowRunType: {testMappingName: {"apiVersion": "2026-03-10"}},
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

// apiVersionRecorder is a fake GitHub API serving one repository, its languages and one workflow
// run. Every payload echoes the X-GitHub-Api-Version of its request, and every request is
// recorded as path@version.
type apiVersionRecorder struct {
	lock     sync.Mutex
	requests []string
}

func (r *apiVersionRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	version := req.Header.Get("X-GitHub-Api-Version")
	r.lock.Lock()
	r.requests = append(r.requests, req.URL.Path+"@"+version)
	r.lock.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch req.URL.Path {
	case "/orgs/test-org/repos":
		json.NewEncoder(w).Encode([]map[string]any{recordedRepository(version)})
	case "/repos/test-org/repo1/languages":
		json.NewEncoder(w).Encode(map[string]any{"Go": 100})
	case "/repos/test-org/repo1/actions/runs":
		json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "workflow_runs": []map[string]any{recordedWorkflowRun(version)}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// recordedRepository is the repository apiVersionRecorder lists with apiVersion.
func recordedRepository(apiVersion string) map[string]any {
	return map[string]any{
		"id":         float64(1),
		"name":       "repo1",
		"full_name":  "test-org/repo1",
		"owner":      map[string]any{"login": "test-org"},
		"listedWith": apiVersion,
	}
}

// recordedWorkflowRun is the workflow run apiVersionRecorder serves with apiVersion.
func recordedWorkflowRun(apiVersion string) map[string]any {
	return map[string]any{"id": float64(10), "fetchedWith": apiVersion}
}

// newRecordedSource returns a Source whose client points at a server backed by recorder.
func newRecordedSource(t *testing.T, recorder *apiVersionRecorder) *Source {
	t.Helper()

	server := httptest.NewServer(recorder)
	t.Cleanup(server.Close)
	return &Source{
		config: config{URL: server.URL, Org: "test-org", Token: "test-token"},
		client: &client{
			baseURL:    server.URL,
			org:        "test-org",
			token:      "test-token",
			pageSize:   100,
			httpClient: server.Client(),
		},
	}
}

func TestStartSyncProcessAPIVersionGroups(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	repositoryData := func(listedWith string, mappings []string) source.Data {
		return source.Data{
			Type:      repositoryType,
			Operation: source.DataOperationUpsert,
			Values: map[string]any{
				repositoryType:        recordedRepository(listedWith),
				"repositoryLanguages": map[string]float64{"Go": 100},
			},
			Time:     fixedTime,
			Mappings: mappings,
		}
	}
	workflowRunData := func(fetchedWith string, mappings []string) source.Data {
		return source.Data{
			Type:      workflowRunType,
			Operation: source.DataOperationUpsert,
			Values:    map[string]any{workflowRunType: recordedWorkflowRun(fetchedWith)},
			Time:      fixedTime,
			Mappings:  mappings,
		}
	}

	testCases := map[string]struct {
		typesToSync      map[string]source.MappingExtras
		expectedRequests []string
		expectedData     []source.Data
	}{
		"repository mappings on two versions list once per version and runs are fetched in the first listing only": {
			typesToSync: map[string]source.MappingExtras{
				repositoryType: {
					"b-repos": {apiVersionKey: "2026-03-10"},
					"a-repos": {apiVersionKey: "2024-01-01"},
				},
				workflowRunType: {"my-runs": {apiVersionKey: "2025-01-01"}},
			},
			expectedRequests: []string{
				"/orgs/test-org/repos@2024-01-01",
				"/repos/test-org/repo1/languages@2024-01-01",
				"/repos/test-org/repo1/actions/runs@2025-01-01",
				"/orgs/test-org/repos@2026-03-10",
				"/repos/test-org/repo1/languages@2026-03-10",
			},
			expectedData: []source.Data{
				repositoryData("2024-01-01", []string{"a-repos"}),
				workflowRunData("2025-01-01", nil),
				repositoryData("2026-03-10", []string{"b-repos"}),
			},
		},
		"workflow run mappings on two versions fetch runs once per version": {
			typesToSync: map[string]source.MappingExtras{
				repositoryType: {"my-repos": {apiVersionKey: "2026-03-10"}},
				workflowRunType: {
					"a-runs": {apiVersionKey: "2024-01-01"},
					"b-runs": {apiVersionKey: "2025-01-01"},
				},
			},
			expectedRequests: []string{
				"/orgs/test-org/repos@2026-03-10",
				"/repos/test-org/repo1/languages@2026-03-10",
				"/repos/test-org/repo1/actions/runs@2024-01-01",
				"/repos/test-org/repo1/actions/runs@2025-01-01",
			},
			expectedData: []source.Data{
				repositoryData("2026-03-10", nil),
				workflowRunData("2024-01-01", []string{"a-runs"}),
				workflowRunData("2025-01-01", []string{"b-runs"}),
			},
		},
		"only workflow runs requested list once with the first run version": {
			typesToSync: map[string]source.MappingExtras{
				workflowRunType: {
					"b-runs": {apiVersionKey: "2025-01-01"},
					"a-runs": {apiVersionKey: "2024-01-01"},
				},
			},
			expectedRequests: []string{
				"/orgs/test-org/repos@2024-01-01",
				"/repos/test-org/repo1/actions/runs@2024-01-01",
				"/repos/test-org/repo1/actions/runs@2025-01-01",
			},
			expectedData: []source.Data{
				workflowRunData("2024-01-01", []string{"a-runs"}),
				workflowRunData("2025-01-01", []string{"b-runs"}),
			},
		},
		"a mapping without version joins the default version group and stays untargeted": {
			typesToSync: map[string]source.MappingExtras{
				repositoryType: {
					"a-repos": nil,
					"b-repos": {apiVersionKey: defaultAPIVersion},
				},
			},
			expectedRequests: []string{
				"/orgs/test-org/repos@" + defaultAPIVersion,
				"/repos/test-org/repo1/languages@" + defaultAPIVersion,
			},
			expectedData: []source.Data{repositoryData(defaultAPIVersion, nil)},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			recorder := &apiVersionRecorder{}
			s := newRecordedSource(t, recorder)

			results := make(chan source.Data, 100)
			err := s.StartSyncProcess(t.Context(), tc.typesToSync, results)
			close(results)
			require.NoError(t, err)

			var got []source.Data
			for d := range results {
				got = append(got, d)
			}

			require.Equal(t, tc.expectedRequests, recorder.requests)
			require.Equal(t, tc.expectedData, got)
		})
	}
}

func TestRepositoryEmissions(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	repoObject := map[string]any{"id": float64(1), "name": "repo1", "full_name": "test-org/repo1"}
	withLanguages := func(mappings []string) source.Data {
		return source.Data{
			Type:      repositoryType,
			Operation: source.DataOperationDelete,
			Values: map[string]any{
				repositoryType:        repoObject,
				"repositoryLanguages": map[string]float64{"Go": 100},
			},
			Time:     fixedTime,
			Mappings: mappings,
		}
	}

	testCases := map[string]struct {
		withClient       bool
		repoObject       map[string]any
		extras           source.MappingExtras
		expectedRequests []string
		expectedData     []source.Data
	}{
		"two versions fetch languages once per version and target each group": {
			withClient: true,
			repoObject: repoObject,
			extras: source.MappingExtras{
				"b-repos": {apiVersionKey: "2026-03-10"},
				"a-repos": {apiVersionKey: "2024-01-01"},
			},
			expectedRequests: []string{
				"/repos/test-org/repo1/languages@2024-01-01",
				"/repos/test-org/repo1/languages@2026-03-10",
			},
			expectedData: []source.Data{
				withLanguages([]string{"a-repos"}),
				withLanguages([]string{"b-repos"}),
			},
		},
		"a single version stays untargeted": {
			withClient: true,
			repoObject: repoObject,
			extras: source.MappingExtras{
				"a-repos": nil,
				"b-repos": {apiVersionKey: defaultAPIVersion},
			},
			expectedRequests: []string{"/repos/test-org/repo1/languages@" + defaultAPIVersion},
			expectedData:     []source.Data{withLanguages(nil)},
		},
		"without a client a single untargeted emission is built": {
			repoObject: repoObject,
			extras: source.MappingExtras{
				"a-repos": {apiVersionKey: "2024-01-01"},
				"b-repos": {apiVersionKey: "2026-03-10"},
			},
			expectedData: []source.Data{{
				Type:      repositoryType,
				Operation: source.DataOperationDelete,
				Values:    map[string]any{repositoryType: repoObject},
				Time:      fixedTime,
			}},
		},
		"without a full name a single untargeted emission is built": {
			withClient: true,
			repoObject: map[string]any{"id": float64(1)},
			extras: source.MappingExtras{
				"a-repos": {apiVersionKey: "2024-01-01"},
				"b-repos": {apiVersionKey: "2026-03-10"},
			},
			expectedData: []source.Data{{
				Type:      repositoryType,
				Operation: source.DataOperationDelete,
				Values:    map[string]any{repositoryType: map[string]any{"id": float64(1)}},
				Time:      fixedTime,
			}},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			recorder := &apiVersionRecorder{}
			var c *client
			if tc.withClient {
				c = newRecordedSource(t, recorder).client
			}

			data := repositoryEmissions(t.Context(), c, tc.extras, tc.repoObject, source.DataOperationDelete)
			require.Equal(t, tc.expectedRequests, recorder.requests)
			require.Equal(t, tc.expectedData, data)
		})
	}
}

func TestWarnUnusableAPIVersions(t *testing.T) {
	t.Parallel()

	logs := &bytes.Buffer{}
	warnUnusableAPIVersions(logger.NewLogger(logs), map[string]source.MappingExtras{
		repositoryType: {
			"a-repos": {apiVersionKey: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)},
			"b-repos": {apiVersionKey: defaultAPIVersion},
		},
		workflowRunType: {"my-runs": {apiVersionKey: ""}},
		// a type whose apiVersion is never used is not checked
		personalAccessTokenRequestType: {"my-requests": {apiVersionKey: 20260310}},
	}, repositoryType, workflowRunType)

	output := logs.String()
	require.Equal(t, 2, strings.Count(output, "mapping apiVersion is not a non-empty string"))
	require.Contains(t, output, `"mapping":"a-repos"`)
	require.Contains(t, output, `"valueType":"time.Time"`)
	require.Contains(t, output, `"mapping":"my-runs"`)
	require.NotContains(t, output, "my-requests")
}
