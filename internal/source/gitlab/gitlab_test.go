// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

// newTestGitLabClient returns a gitLabClient pointing at the provided test server.
func newTestGitLabClient(t *testing.T, srv *httptest.Server) *gitLabClient {
	t.Helper()
	return &gitLabClient{
		config: sourceConfig{
			Token:   "test-token",
			BaseURL: srv.URL,
		},
		http: srv.Client(),
	}
}

// jsonResponse writes a JSON-encoded value with status 200 to w.
func jsonResponse(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

// -------------------------------------------------------------------
// Env / config tests
// -------------------------------------------------------------------

func TestLoadSourceConfigFromEnv(t *testing.T) {
	testCases := map[string]struct {
		setEnv      func(t *testing.T)
		expectedCfg sourceConfig
		expectErr   bool
	}{
		"all env set": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_TOKEN", "my-token")
				t.Setenv("GITLAB_BASE_URL", "https://gitlab.example.com")
			},
			expectedCfg: sourceConfig{Token: "my-token", BaseURL: "https://gitlab.example.com"},
		},
		"default base URL": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_TOKEN", "my-token")
			},
			expectedCfg: sourceConfig{Token: "my-token", BaseURL: "https://gitlab.com"},
		},
		"missing required token": {
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			if tc.setEnv != nil {
				tc.setEnv(t)
			}

			cfg, err := loadSourceConfigFromEnv()
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedCfg, cfg)
		})
	}
}

func TestLoadWebhookConfigFromEnv(t *testing.T) {
	testCases := map[string]struct {
		setEnv      func(t *testing.T)
		expectedCfg webhookConfig
		expectErr   bool
	}{
		"all env set": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_WEBHOOK_PATH", "/hooks/gitlab")
				t.Setenv("GITLAB_WEBHOOK_TOKEN", "secret")
			},
			expectedCfg: webhookConfig{WebhookPath: "/hooks/gitlab", WebhookToken: "secret"},
		},
		"default path": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_WEBHOOK_TOKEN", "secret")
			},
			expectedCfg: webhookConfig{WebhookPath: "/gitlab/webhook", WebhookToken: "secret"},
		},
		"no token is valid": {
			expectedCfg: webhookConfig{WebhookPath: "/gitlab/webhook"},
		},
		"path without leading slash": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_WEBHOOK_PATH", "hooks/gitlab")
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			if tc.setEnv != nil {
				tc.setEnv(t)
			}

			cfg, err := loadWebhookConfigFromEnv()
			if tc.expectErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrWebhookConfigNotValid)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedCfg, cfg)
		})
	}
}

// -------------------------------------------------------------------
// HTTP client tests
// -------------------------------------------------------------------

func TestMakeRequest(t *testing.T) {
	testCases := map[string]struct {
		handler       http.HandlerFunc
		expectedItems []map[string]any
		expectedCurr  int
		expectedTotal int
		expectErr     bool
	}{
		"successful single page": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "test-token", r.Header.Get("PRIVATE-TOKEN"))
				assert.Equal(t, "application/json", r.Header.Get("Accept"))
				assert.Equal(t, "1", r.URL.Query().Get("page"))
				w.Header().Set("x-page", "1")
				w.Header().Set("x-total-pages", "1")
				jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
			},
			expectedItems: []map[string]any{{"id": float64(1)}},
			expectedCurr:  1,
			expectedTotal: 1,
		},
		"pagination headers parsed": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-page", "2")
				w.Header().Set("x-total-pages", "5")
				jsonResponse(t, w, []map[string]any{{"id": float64(2)}})
			},
			expectedItems: []map[string]any{{"id": float64(2)}},
			expectedCurr:  2,
			expectedTotal: 5,
		},
		"non-200 status": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectErr: true,
		},
		"malformed JSON": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-page", "1")
				w.Header().Set("x-total-pages", "1")
				_, _ = w.Write([]byte("not-json"))
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestGitLabClient(t, srv)
			items, currPage, totalPages, err := client.makeRequest(t.Context(), "/api/v4/projects", "per_page=100", 1)

			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedItems, items)
			assert.Equal(t, tc.expectedCurr, currPage)
			assert.Equal(t, tc.expectedTotal, totalPages)
		})
	}
}

func TestListAllPages(t *testing.T) {
	testCases := map[string]struct {
		pages         int // total pages the server reports
		expectedCount int
		expectErr     bool
	}{
		"single page": {
			pages:         1,
			expectedCount: 1,
		},
		"three pages all collected": {
			pages:         3,
			expectedCount: 3,
		},
		"stops at maxPagesLimit": {
			pages:         maxPagesLimit + 5,
			expectedCount: maxPagesLimit,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var requestCount atomic.Int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := int(requestCount.Add(1))
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				w.Header().Set("x-page", strconv.Itoa(page))
				w.Header().Set("x-total-pages", strconv.Itoa(tc.pages))
				jsonResponse(t, w, []map[string]any{{"page": float64(n)}})
			}))
			defer srv.Close()

			client := newTestGitLabClient(t, srv)
			items, err := client.listAllPages(t.Context(), "/api/v4/projects", "per_page=100")

			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, items, tc.expectedCount)
			assert.EqualValues(t, tc.expectedCount, requestCount.Load())
		})
	}
}

func TestListProjects(t *testing.T) {
	testCases := map[string]struct {
		handler       http.HandlerFunc
		expectedCount int
		expectErr     bool
	}{
		"success single page": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-page", "1")
				w.Header().Set("x-total-pages", "1")
				jsonResponse(t, w, []map[string]any{{"id": float64(1), "name": "repo"}})
			},
			expectedCount: 1,
		},
		"success multi page": {
			handler: func() http.HandlerFunc {
				var call atomic.Int32
				return func(w http.ResponseWriter, r *http.Request) {
					n := int(call.Add(1))
					w.Header().Set("x-page", strconv.Itoa(n))
					w.Header().Set("x-total-pages", "2")
					jsonResponse(t, w, []map[string]any{{"id": float64(n)}})
				}
			}(),
			expectedCount: 2,
		},
		"server error": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestGitLabClient(t, srv)
			items, err := client.ListProjects(t.Context())

			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, items, tc.expectedCount)
		})
	}
}

func TestListPipelines(t *testing.T) {
	testCases := map[string]struct {
		handler       http.HandlerFunc
		projectID     string
		expectedCount int
		expectErr     bool
	}{
		"success": {
			projectID: "42",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "/42/pipelines")
				w.Header().Set("x-page", "1")
				w.Header().Set("x-total-pages", "1")
				jsonResponse(t, w, []map[string]any{{"id": float64(10)}, {"id": float64(11)}})
			},
			expectedCount: 2,
		},
		"server error": {
			projectID: "42",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestGitLabClient(t, srv)
			items, err := client.ListPipelines(t.Context(), tc.projectID)

			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, items, tc.expectedCount)
		})
	}
}

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
			handler: paginatedHandler(t, map[string]any{"/api/v4/projects": singleProject}),
			typesToSync: map[string]source.Extra{
				projectResource: nil,
			},
			expectedDataCount: 1,
			checkData: func(t *testing.T, data []source.Data) {
				t.Helper()
				assert.Equal(t, projectResource, data[0].Type)
				assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
				assert.Equal(t, singleProject[0], data[0].Values)
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
				"/api/v4/projects":             singleProject,
				"/api/v4/projects/1/pipelines": singlePipeline,
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

// paginatedHandler returns an http.HandlerFunc that serves pre-canned JSON arrays
// for the given path map. Responses always include x-page:1 / x-total-pages:1.
func paginatedHandler(t *testing.T, routes map[string]any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		data, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("x-page", "1")
		w.Header().Set("x-total-pages", "1")
		jsonResponse(t, w, data)
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
	expectedTime, _ := time.Parse(time.RFC3339, validUpdatedAt)

	validBody := func(updatedAt string) []byte {
		payload := map[string]any{
			"object_kind": "pipeline",
			"object_attributes": map[string]any{
				"id":     float64(31),
				"status": "success",
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
			results := make(chan source.Data, 1)

			s := &Source{
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
// pipelineEvent / event.go tests
// -------------------------------------------------------------------

func TestPipelineEvent_EventTime(t *testing.T) {
	testCases := map[string]struct {
		objectAttributes map[string]any
		checkTime        func(t *testing.T, got time.Time)
	}{
		"updated_at present and parseable": {
			objectAttributes: map[string]any{"updated_at": "2024-05-15T08:30:00Z"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				expected, _ := time.Parse(time.RFC3339, "2024-05-15T08:30:00Z")
				assert.Equal(t, expected.UTC(), got.UTC())
			},
		},
		"updated_at present but malformed falls back to now": {
			objectAttributes: map[string]any{"updated_at": "not-a-date"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
		"updated_at absent falls back to now": {
			objectAttributes: map[string]any{"status": "success"},
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
		"nil object_attributes falls back to now": {
			objectAttributes: nil,
			checkTime: func(t *testing.T, got time.Time) {
				t.Helper()
				assert.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ev := &pipelineEvent{ObjectAttributes: tc.objectAttributes}
			tc.checkTime(t, ev.EventTime())
		})
	}
}

func TestParsePipelineEvent(t *testing.T) {
	testCases := map[string]struct {
		body          []byte
		expectKind    string
		expectValKeys []string
		expectErr     bool
	}{
		"full pipeline payload": {
			body: mustMarshal(t, map[string]any{
				"object_kind":       "pipeline",
				"object_attributes": map[string]any{"id": float64(1), "status": "running"},
				"project":           map[string]any{"id": float64(5)},
			}),
			expectKind:    "pipeline",
			expectValKeys: []string{"object_kind", "object_attributes", "project"},
		},
		"invalid json": {
			body:      []byte("{bad json"),
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ev, err := parsePipelineEvent(tc.body)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectKind, ev.ObjectKind)
			for _, k := range tc.expectValKeys {
				assert.Contains(t, ev.ToValues(), k)
			}
		})
	}
}

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
			id, err := projectIDFromItem(tc.item)
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
// helpers
// -------------------------------------------------------------------

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func Test_RealRequestDeveloping(t *testing.T) {
	t.Skip("This test is meant to be used for development purposes, it makes real API calls to GitLab and doesn't have assertions")

	tokenRelativePath := "../../../local/gitlab/personal_access_token"
	tokenBytes, err := os.ReadFile(tokenRelativePath)
	require.NoError(t, err, "failed to read GitLab token from file: %s", tokenRelativePath)
	token := strings.TrimSpace(string(tokenBytes))

	// To run: remove t.Skip, set GITLAB_TOKEN env var or populate the file below.
	t.Setenv("GITLAB_TOKEN", token)
	t.Setenv("GITLAB_BASE_URL", "https://git.tools.mia-platform.eu")

	s, err := NewSource()
	require.NoError(t, err)

	results := make(chan source.Data, 100)
	err = s.StartSyncProcess(t.Context(), map[string]source.Extra{projectResource: nil}, results)
	require.NoError(t, err)
	close(results)

	for d := range results {
		fmt.Printf("type=%s id=%v\n", d.Type, d.Values["id"])
	}
}
