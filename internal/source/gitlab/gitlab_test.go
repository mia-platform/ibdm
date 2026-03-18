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
		"sync pipelines": {
			handler: paginatedHandler(t, map[string]any{
				"/api/v4/projects":             singleProject,
				"/api/v4/projects/1/pipelines": singlePipeline,
			}),
			typesToSync: map[string]source.Extra{
				pipelineResource: nil,
			},
			expectedDataCount: 1,
			checkData: func(t *testing.T, data []source.Data) {
				t.Helper()
				assert.Equal(t, pipelineResource, data[0].Type)
				assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
				assert.Equal(t, singlePipeline[0], data[0].Values)
			},
		},
		"sync projects and pipelines": {
			handler: paginatedHandler(t, map[string]any{
				"/api/v4/projects":                 singleProject,
				"/api/v4/projects/1/pipelines":     singlePipeline,
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
	validUpdatedAt := "2024-06-01T10:00:00Z"
	validProjectID := float64(5)
	validProject := map[string]any{
		"id":   validProjectID,
		"name": "test-project",
	}
	expectedTime, _ := time.Parse(time.RFC3339, validUpdatedAt)

	validBody := func(updatedAt string) []byte {
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
		if updatedAt != "" {
			payload["object_attributes"].(map[string]any)["updated_at"] = updatedAt
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
			body:          validBody(""),
			headers:       validHeaders(validToken, pipelineHookHeaderValue),
			typesToStream: map[string]source.Extra{pipelineResource: nil},
			expectData:    true,
			checkData: func(t *testing.T, d source.Data) {
				t.Helper()
				assert.Equal(t, pipelineResource, d.Type)
				assert.Equal(t, source.DataOperationUpsert, d.Operation)
				assert.NotNil(t, d.Values)
			},
		},
		"valid pipeline event with updated_at time": {
			token:         validToken,
			body:          validBody(validUpdatedAt),
			headers:       validHeaders(validToken, pipelineHookHeaderValue),
			typesToStream: map[string]source.Extra{pipelineResource: nil},
			expectData:    true,
			checkData: func(t *testing.T, d source.Data) {
				t.Helper()
				assert.Equal(t, expectedTime.UTC(), d.Time.UTC())
			},
		},
		"invalid token": {
			token:         validToken,
			body:          validBody(""),
			headers:       validHeaders("wrong-token", pipelineHookHeaderValue),
			typesToStream: map[string]source.Extra{pipelineResource: nil},
			expectErr:     ErrSignatureMismatch,
		},
		"wrong event type is silently ignored": {
			token:         validToken,
			body:          validBody(""),
			headers:       validHeaders(validToken, "Push Hook"),
			typesToStream: map[string]source.Extra{pipelineResource: nil},
			expectData:    false,
		},
		"malformed body": {
			token:         validToken,
			body:          []byte("not-json"),
			headers:       validHeaders(validToken, pipelineHookHeaderValue),
			typesToStream: map[string]source.Extra{pipelineResource: nil},
			expectErr:     ErrUnmarshalingEvent,
		},
		"pipeline type not in typesToStream": {
			token:         validToken,
			body:          validBody(""),
			headers:       validHeaders(validToken, pipelineHookHeaderValue),
			typesToStream: map[string]source.Extra{"project": nil},
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
				case "/api/v4/projects/5/access_tokens":
					jsonResponse(t, w, []map[string]any{})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			results := make(chan source.Data, 1)

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
				assert.Empty(t, results)
				return
			}

			select {
			case data := <-results:
				if tc.checkData != nil {
					tc.checkData(t, data)
				}
			case <-t.Context().Done():
				t.Fatal("timed out waiting for pipeline event to be dispatched")
			}
		})
	}
}

// -------------------------------------------------------------------
// parsePipelineEvent tests
// -------------------------------------------------------------------

func TestParsePipelineEvent(t *testing.T) {
	projectPayload := map[string]any{
		"id":                  float64(5),
		"name":                "test-project",
		"path_with_namespace": "group/test-project",
	}

	testCases := map[string]struct {
		body          []byte
		handler       http.HandlerFunc
		expectKind    string
		expectValKeys []string
		expectProject map[string]any
		expectErr     bool
	}{
		"full pipeline payload": {
			body: mustMarshal(t, map[string]any{
				"object_kind": "pipeline",
				"object_attributes": map[string]any{
					"id":     float64(1),
					"status": "running",
				},
				"project": map[string]any{
					"id": float64(5),
				},
			}),
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v4/projects/5":
					jsonResponse(t, w, projectPayload)
				case "/api/v4/projects/5/languages":
					jsonResponse(t, w, map[string]any{"Go": 100.0})
				case "/api/v4/projects/5/access_tokens":
					jsonResponse(t, w, []map[string]any{})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			expectKind:    "pipeline",
			expectValKeys: []string{"object_kind", "object_attributes", "project"},
			expectProject: map[string]any{
				"project":               projectPayload,
				"project_languages":     map[string]any{"Go": 100.0},
				"project_access_tokens": []map[string]any{},
			},
		},
		"invalid json": {
			body: []byte("{bad json"),
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectErr: true,
		},
		"getProject API error": {
			body: mustMarshal(t, map[string]any{
				"object_kind": "pipeline",
				"object_attributes": map[string]any{
					"id": float64(1),
				},
				"project": map[string]any{
					"id": float64(5),
				},
			}),
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
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

			ev, err := s.parsePipelineEvent(t.Context(), tc.body)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectKind, ev.ObjectKind)
			for _, k := range tc.expectValKeys {
				assert.Contains(t, ev.ToValues(), k)
			}
			if tc.expectProject != nil {
				assert.Equal(t, tc.expectProject, ev.project)
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
