// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestPushEvent_EventTime(t *testing.T) {
	ev := &pushEvent{}
	assert.WithinDuration(t, time.Now(), ev.EventTime(), 5*time.Second)
}

func TestParsePushEvent(t *testing.T) {
	projectPayload := map[string]any{
		"id":                  float64(5),
		"name":                "test-project",
		"path_with_namespace": "group/test-project",
	}

	testCases := map[string]struct {
		body          []byte
		handler       http.HandlerFunc
		expectKind    string
		expectProject map[string]any
		expectErr     bool
	}{
		"full push payload": {
			body: mustMarshal(t, map[string]any{
				"object_kind": "push",
				"project_id":  float64(5),
			}),
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v4/projects/5":
					jsonResponse(t, w, projectPayload)
				case "/api/v4/projects/5/languages":
					jsonResponse(t, w, map[string]any{"Go": 100.0})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			expectKind: "push",
			expectProject: map[string]any{
				"project":           projectPayload,
				"project_languages": map[string]any{"Go": 100.0},
			},
		},
		"invalid json": {
			body: []byte("{bad json"),
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectErr: true,
		},
		"missing project_id": {
			body: mustMarshal(t, map[string]any{
				"object_kind": "push",
			}),
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectErr: true,
		},
		"getProject API error": {
			body: mustMarshal(t, map[string]any{
				"object_kind": "push",
				"project_id":  float64(5),
			}),
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectErr: true,
		},
		"getProjectLanguages API error": {
			body: mustMarshal(t, map[string]any{
				"object_kind": "push",
				"project_id":  float64(5),
			}),
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v4/projects/5":
					jsonResponse(t, w, projectPayload)
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			c := newTestGitLabClient(t, srv)

			ev, err := parsePushEvent(t.Context(), c, tc.body)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectKind, ev.ObjectKind)
			if tc.expectProject != nil {
				assert.Equal(t, tc.expectProject, ev.project)
			}
		})
	}
}

func TestPushEventProcessor(t *testing.T) {
	validProjectID := float64(5)
	validProject := map[string]any{
		"id":   validProjectID,
		"name": "test-project",
	}

	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/5":
			jsonResponse(t, w, validProject)
		case "/api/v4/projects/5/languages":
			jsonResponse(t, w, map[string]any{"Go": 100.0})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	pushBody := mustMarshal(t, map[string]any{
		"object_kind": "push",
		"project_id":  validProjectID,
	})

	testCases := map[string]struct {
		body          []byte
		typesToStream map[string]source.Extra
		expectData    int
		checkData     func(t *testing.T, data []source.Data)
		expectErr     bool
	}{
		"project in typesToStream": {
			body:          pushBody,
			typesToStream: map[string]source.Extra{projectResource: nil},
			expectData:    1,
			checkData: func(t *testing.T, data []source.Data) {
				t.Helper()
				assert.Equal(t, projectResource, data[0].Type)
				assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
				assert.NotNil(t, data[0].Values)
			},
		},
		"wrong object_kind returns no data": {
			body: mustMarshal(t, map[string]any{
				"object_kind": "pipeline",
				"project_id":  validProjectID,
			}),
			typesToStream: map[string]source.Extra{projectResource: nil},
			expectData:    0,
		},
		"project not in typesToStream returns no data": {
			body:          pushBody,
			typesToStream: map[string]source.Extra{pipelineResource: nil},
			expectData:    0,
		},
		"malformed body": {
			body:          []byte("not-json"),
			typesToStream: map[string]source.Extra{projectResource: nil},
			expectErr:     true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(apiHandler)
			defer srv.Close()

			c := newTestGitLabClient(t, srv)
			p := &pushEventProcessor{}

			data, err := p.process(t.Context(), c, tc.typesToStream, tc.body)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, data, tc.expectData)
			if tc.checkData != nil {
				tc.checkData(t, data)
			}
		})
	}
}
