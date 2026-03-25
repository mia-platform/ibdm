// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

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
