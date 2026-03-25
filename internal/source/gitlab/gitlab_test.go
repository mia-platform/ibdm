// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

// -------------------------------------------------------------------
// StartSyncProcess tests
// -------------------------------------------------------------------

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

// -------------------------------------------------------------------
// GetWebhook / handler tests
// -------------------------------------------------------------------

func TestGetWebhook_MissingToken(t *testing.T) {
	s := &Source{
		webhookConfig: webhookConfig{WebhookPath: "/gitlab/webhook", WebhookToken: ""},
	}
	_, err := s.GetWebhook(t.Context(), map[string]source.Extra{pipelineResource: nil}, make(chan source.Data))
	require.ErrorIs(t, err, ErrWebhookTokenMissing)
}

func TestGetWebhook_ReturnsCorrectPathAndMethod(t *testing.T) {
	s := &Source{
		webhookConfig: webhookConfig{WebhookPath: "/hooks/gitlab", WebhookToken: "secret"},
	}

	webhook, err := s.GetWebhook(t.Context(), map[string]source.Extra{pipelineResource: nil}, make(chan source.Data, 1))
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, webhook.Method)
	assert.Equal(t, "/hooks/gitlab", webhook.Path)
}

func TestWebhookHandler(t *testing.T) {
	validToken := "super-secret"
	validProjectID := float64(5)
	validProject := map[string]any{
		"id":   validProjectID,
		"name": "test-project",
	}

	validBody := func() []byte {
		payload := map[string]any{
			"object_kind": "pipeline",
			"object_attributes": map[string]any{
				"id":     float64(31),
				"status": "success",
			},
			"project": map[string]any{
				"id": validProjectID,
			},
		}
		b, _ := json.Marshal(payload)
		return b
	}

	validHeaders := func(token, event string) http.Header {
		h := http.Header{}
		h.Set(gitlabTokenHeader, token)
		h.Set(gitlabEventHeader, event)
		return h
	}

	testCases := map[string]struct {
		token         string
		body          []byte
		headers       http.Header
		typesToStream map[string]source.Extra
		expectErr     error
		expectData    bool
		checkData     func(t *testing.T, d source.Data)
	}{
		"valid pipeline event dispatched": {
			token:         validToken,
			body:          validBody(),
			headers:       validHeaders(validToken, pipelineHookHeaderValue),
			typesToStream: map[string]source.Extra{projectResource: nil, pipelineResource: nil},
			expectData:    true,
			checkData: func(t *testing.T, d source.Data) {
				t.Helper()
				assert.Equal(t, projectResource, d.Type)
				assert.Equal(t, source.DataOperationUpsert, d.Operation)
				assert.NotNil(t, d.Values)
			},
		},
		"invalid token": {
			token:         validToken,
			body:          validBody(),
			headers:       validHeaders("wrong-token", pipelineHookHeaderValue),
			typesToStream: map[string]source.Extra{projectResource: nil, pipelineResource: nil},
			expectErr:     ErrSignatureMismatch,
		},
		"unknown event type is silently ignored": {
			token:         validToken,
			body:          validBody(),
			headers:       validHeaders(validToken, "Push Hook"),
			typesToStream: map[string]source.Extra{projectResource: nil, pipelineResource: nil},
			expectData:    false,
		},
		"processor error does not produce data": {
			token:         validToken,
			body:          []byte("not-json"),
			headers:       validHeaders(validToken, pipelineHookHeaderValue),
			typesToStream: map[string]source.Extra{projectResource: nil, pipelineResource: nil},
			expectData:    false,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v4/projects/5":
					jsonResponse(t, w, validProject)
				case "/api/v4/projects/5/languages":
					jsonResponse(t, w, map[string]any{"Go": 100.0})
				case "/api/v4/projects/5/pipelines/31":
					jsonResponse(t, w, map[string]any{"id": float64(31), "status": "success"})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			results := make(chan source.Data, 2)

			s := &Source{
				c: newTestGitLabClient(t, srv),
				webhookConfig: webhookConfig{
					WebhookPath:  "/gitlab/webhook",
					WebhookToken: tc.token,
				},
			}

			webhook, err := s.GetWebhook(t.Context(), tc.typesToStream, results)
			require.NoError(t, err)

			handlerErr := webhook.Handler(t.Context(), tc.headers, tc.body)

			if tc.expectErr != nil {
				require.ErrorIs(t, handlerErr, tc.expectErr)
				return
			}
			require.NoError(t, handlerErr)

			if !tc.expectData {
				time.Sleep(100 * time.Millisecond)
				assert.Empty(t, results)
				return
			}

			select {
			case data := <-results:
				if tc.checkData != nil {
					tc.checkData(t, data)
				}
			case <-t.Context().Done():
				t.Fatal("timed out waiting for webhook event to be dispatched")
			}
		})
	}
}

// -------------------------------------------------------------------
// Utility tests
// -------------------------------------------------------------------

func TestUpdatedAtOrNow(t *testing.T) {
	testCases := map[string]struct {
		item      map[string]any
		checkTime func(t *testing.T, got time.Time)
	}{
		"valid updated_at": {
			item: map[string]any{"updated_at": "2023-11-01T00:00:00Z"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				expected, _ := time.Parse(time.RFC3339, "2023-11-01T00:00:00Z")
				assert.Equal(t, expected.UTC(), got.UTC())
			},
		},
		"invalid updated_at falls back to now": {
			item: map[string]any{"updated_at": "garbage"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
		"missing updated_at falls back to now": {
			item: map[string]any{"name": "project"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			tc.checkTime(t, updatedAtOrNow(tc.item))
		})
	}
}

func TestProjectIDFromItem(t *testing.T) {
	testCases := map[string]struct {
		item      map[string]any
		expectID  string
		expectErr bool
	}{
		"valid numeric id": {
			item:     map[string]any{"id": float64(123)},
			expectID: "123",
		},
		"large numeric id": {
			item:     map[string]any{"id": float64(9876543)},
			expectID: "9876543",
		},
		"missing id": {
			item:      map[string]any{"name": "project"},
			expectErr: true,
		},
		"string id not supported": {
			item:      map[string]any{"id": "abc"},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			id, err := getIDFromItem(tc.item)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectID, id)
		})
	}
}

// -------------------------------------------------------------------
// Development helpers
// -------------------------------------------------------------------

// TODO: remove before merging PR.
func Test_RealRequestDeveloping(t *testing.T) {
	t.Skip("This test is meant to be used for development purposes, it makes real API calls to GitLab and doesn't have assertions")

	basePath := "../../../local/gitlab/"
	tokenRelativePath := basePath + "personal_access_token"
	tokenBytes, err := os.ReadFile(tokenRelativePath)
	require.NoError(t, err, "failed to read GitLab token from file: %s", tokenRelativePath)
	token := strings.TrimSpace(string(tokenBytes))

	baseURLRelativePath := basePath + "base_url"
	baseURLBytes, err := os.ReadFile(baseURLRelativePath)
	require.NoError(t, err, "failed to read GitLab base URL from file: %s", baseURLRelativePath)
	baseURL := strings.TrimSpace(string(baseURLBytes))

	// To run: remove t.Skip, set GITLAB_TOKEN env var or populate the file below.
	t.Setenv("GITLAB_TOKEN", token)
	t.Setenv("GITLAB_BASE_URL", baseURL)

	s, err := NewSource()
	require.NoError(t, err)

	results := make(chan source.Data, 400)
	err = s.StartSyncProcess(t.Context(), map[string]source.Extra{projectResource: nil}, results)
	require.NoError(t, err)
	close(results)

	require.Len(t, results, 300)
}
