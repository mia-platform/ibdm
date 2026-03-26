// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestStartSyncProcess(t *testing.T) {
	singleProject := []map[string]any{{"id": float64(1), "name": "my-project", "updated_at": "2024-01-01T12:00:00Z"}}
	singlePipeline := []map[string]any{{"id": float64(100), "status": "success", "updated_at": "2024-01-02T12:00:00Z"}}

	testCases := map[string]struct {
		handler           http.HandlerFunc
		typesToSync       map[string]source.Extra
		expectedDataCount int
		checkData         func(t *testing.T, data []source.Data)
		expectErr         bool
		lockBeforeRun     bool
	}{
		"sync projects": {
			handler: paginatedHandler(t, map[string]any{
				"/api/v4/projects":                 singleProject,
				"/api/v4/projects/1/languages":     map[string]any{"Go": 100.0},
				"/api/v4/projects/1/access_tokens": []map[string]any{},
			}),
			typesToSync: map[string]source.Extra{
				projectResource: nil,
			},
			expectedDataCount: 1,
			checkData: func(t *testing.T, data []source.Data) {
				t.Helper()
				assert.Equal(t, projectResource, data[0].Type)
				assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
				innerProject, _ := data[0].Values["project"].(map[string]any)
				assert.Equal(t, "my-project", innerProject["name"])
				assert.Equal(t, map[string]any{"Go": 100.0}, data[0].Values["project_languages"])
			},
		},
		"sync pipelines only without project resource is no-op": {
			handler:           paginatedHandler(t, map[string]any{}),
			typesToSync:       map[string]source.Extra{pipelineResource: nil},
			expectedDataCount: 0,
		},
		"sync projects and pipelines": {
			handler: paginatedHandler(t, map[string]any{
				"/api/v4/projects":                 singleProject,
				"/api/v4/projects/1/pipelines":     singlePipeline,
				"/api/v4/projects/1/pipelines/100": map[string]any{"id": float64(100), "status": "success"},
				"/api/v4/projects/1/languages":     map[string]any{"Go": 100.0},
				"/api/v4/projects/1/access_tokens": []map[string]any{},
			}),
			typesToSync: map[string]source.Extra{
				projectResource:  nil,
				pipelineResource: nil,
			},
			expectedDataCount: 2,
		},
		"unknown type is no-op": {
			handler:           paginatedHandler(t, map[string]any{}),
			typesToSync:       map[string]source.Extra{"unknown": nil},
			expectedDataCount: 0,
		},
		"already running returns early": {
			handler:           paginatedHandler(t, map[string]any{}),
			typesToSync:       map[string]source.Extra{projectResource: nil},
			lockBeforeRun:     true,
			expectedDataCount: 0,
		},
		"sync group access tokens": {
			handler: paginatedHandler(t, map[string]any{
				"/api/v4/groups":                  []map[string]any{{"id": float64(10), "name": "my-group"}},
				"/api/v4/groups/10/access_tokens": []map[string]any{{"id": float64(1), "name": "token-a"}, {"id": float64(2), "name": "token-b"}},
			}),
			typesToSync: map[string]source.Extra{
				accessTokenResource: nil,
			},
			expectedDataCount: 2,
			checkData: func(t *testing.T, data []source.Data) {
				t.Helper()
				for _, d := range data {
					assert.Equal(t, accessTokenResource, d.Type)
					assert.Equal(t, source.DataOperationUpsert, d.Operation)
				}
			},
		},
		"sync projects emits project access tokens": {
			handler: paginatedHandler(t, map[string]any{
				"/api/v4/projects":                 singleProject,
				"/api/v4/projects/1/languages":     map[string]any{"Go": 100.0},
				"/api/v4/projects/1/access_tokens": []map[string]any{{"id": float64(10), "name": "tok-a"}, {"id": float64(11), "name": "tok-b"}},
				"/api/v4/groups":                   []map[string]any{},
			}),
			typesToSync: map[string]source.Extra{
				projectResource:     nil,
				accessTokenResource: nil,
			},
			expectedDataCount: 3, // 1 project + 2 access tokens
			checkData: func(t *testing.T, data []source.Data) {
				t.Helper()
				types := make(map[string]int)
				for _, d := range data {
					types[d.Type]++
					assert.Equal(t, source.DataOperationUpsert, d.Operation)
				}
				assert.Equal(t, 1, types[projectResource])
				assert.Equal(t, 2, types[accessTokenResource])

				var tokenData []source.Data
				for _, d := range data {
					if d.Type == accessTokenResource {
						tokenData = append(tokenData, d)
					}
				}
				for _, d := range tokenData {
					assert.Contains(t, d.Values, "project")
					assert.Contains(t, d.Values, "token")
					innerProject, _ := d.Values["project"].(map[string]any)
					assert.Equal(t, "my-project", innerProject["name"])
				}
			},
		},
		"sync projects access tokens API error": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v4/projects":
					w.Header().Set("x-total-pages", "1")
					jsonResponse(t, w, singleProject)
				case "/api/v4/projects/1/languages":
					jsonResponse(t, w, map[string]any{"Go": 100.0})
				case "/api/v4/projects/1/access_tokens":
					w.WriteHeader(http.StatusInternalServerError)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			typesToSync: map[string]source.Extra{
				projectResource:     nil,
				accessTokenResource: nil,
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			s := &Source{
				c: newTestGitLabClient(t, srv),
			}

			if tc.lockBeforeRun {
				s.syncLock.Lock()
				defer s.syncLock.Unlock()
			}

			results := make(chan source.Data, 10)
			err := s.StartSyncProcess(t.Context(), tc.typesToSync, results)

			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			close(results)

			var received []source.Data
			for d := range results {
				received = append(received, d)
			}

			assert.Len(t, received, tc.expectedDataCount)

			if tc.checkData != nil && len(received) > 0 {
				tc.checkData(t, received)
			}
		})
	}
}
