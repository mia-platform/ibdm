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
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			expectKind: "pipeline",
			expectProject: map[string]any{
				"project":           projectPayload,
				"project_languages": map[string]any{"Go": 100.0},
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

			c := newTestGitLabClient(t, srv)

			ev, err := parsePipelineEvent(t.Context(), c, tc.body)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectKind, ev.ObjectKind)
			assert.NotEmpty(t, ev.pipeline)
			if tc.expectProject != nil {
				assert.Equal(t, tc.expectProject, ev.project)
			}
		})
	}
}

func TestPipelineEventProcessor(t *testing.T) {
	validProjectID := float64(5)
	validProject := map[string]any{
		"id":   validProjectID,
		"name": "test-project",
	}
	validUpdatedAt := "2024-06-01T10:00:00Z"
	expectedTime, _ := time.Parse(time.RFC3339, validUpdatedAt)

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

	pipelineBody := func(updatedAt string) []byte {
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
		return mustMarshal(t, payload)
	}

	testCases := map[string]struct {
		body          []byte
		typesToStream map[string]source.Extra
		expectData    int
		checkData     func(t *testing.T, data []source.Data)
		expectErr     bool
	}{
		"both project and pipeline in typesToStream": {
			body:          pipelineBody(""),
			typesToStream: map[string]source.Extra{projectResource: nil, pipelineResource: nil},
			expectData:    2,
			checkData: func(t *testing.T, data []source.Data) {
				t.Helper()
				assert.Equal(t, projectResource, data[0].Type)
				assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
				assert.NotNil(t, data[0].Values)
				assert.Equal(t, pipelineResource, data[1].Type)
				assert.Equal(t, source.DataOperationUpsert, data[1].Operation)
				assert.NotNil(t, data[1].Values)
			},
		},
		"valid pipeline event with updated_at time": {
			body:          pipelineBody(validUpdatedAt),
			typesToStream: map[string]source.Extra{projectResource: nil, pipelineResource: nil},
			expectData:    2,
			checkData: func(t *testing.T, data []source.Data) {
				t.Helper()
				assert.Equal(t, expectedTime.UTC(), data[0].Time.UTC())
				assert.Equal(t, expectedTime.UTC(), data[1].Time.UTC())
			},
		},
		"wrong object_kind returns no data": {
			body: mustMarshal(t, map[string]any{
				"object_kind":       "push",
				"object_attributes": map[string]any{},
				"project":           map[string]any{"id": validProjectID},
			}),
			typesToStream: map[string]source.Extra{projectResource: nil, pipelineResource: nil},
			expectData:    0,
		},
		"project not in typesToStream returns no data": {
			body:          pipelineBody(""),
			typesToStream: map[string]source.Extra{pipelineResource: nil},
			expectData:    0,
		},
		"pipeline not in typesToStream returns no data": {
			body:          pipelineBody(""),
			typesToStream: map[string]source.Extra{projectResource: nil},
			expectData:    0,
		},
		"malformed body": {
			body:          []byte("not-json"),
			typesToStream: map[string]source.Extra{projectResource: nil, pipelineResource: nil},
			expectErr:     true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(apiHandler)
			defer srv.Close()

			c := newTestGitLabClient(t, srv)
			p := &pipelineEventProcessor{}

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
