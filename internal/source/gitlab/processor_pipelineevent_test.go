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
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			expectKind:    "pipeline",
			expectValKeys: []string{"object_kind", "object_attributes", "project"},
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

			s := &Source{
				c: newTestGitLabClient(t, srv),
			}

			ev, err := parsePipelineEvent(t.Context(), s, tc.body)
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
		"valid pipeline event": {
			body:          pipelineBody(""),
			typesToStream: map[string]source.Extra{pipelineResource: nil},
			expectData:    1,
			checkData: func(t *testing.T, data []source.Data) {
				t.Helper()
				assert.Equal(t, pipelineResource, data[0].Type)
				assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
				assert.NotNil(t, data[0].Values)
			},
		},
		"valid pipeline event with updated_at time": {
			body:          pipelineBody(validUpdatedAt),
			typesToStream: map[string]source.Extra{pipelineResource: nil},
			expectData:    1,
			checkData: func(t *testing.T, data []source.Data) {
				t.Helper()
				assert.Equal(t, expectedTime.UTC(), data[0].Time.UTC())
			},
		},
		"wrong object_kind returns no data": {
			body: mustMarshal(t, map[string]any{
				"object_kind":       "push",
				"object_attributes": map[string]any{},
				"project":           map[string]any{"id": validProjectID},
			}),
			typesToStream: map[string]source.Extra{pipelineResource: nil},
			expectData:    0,
		},
		"pipeline type not in typesToStream": {
			body:          pipelineBody(""),
			typesToStream: map[string]source.Extra{"project": nil},
			expectData:    0,
		},
		"malformed body": {
			body:          []byte("not-json"),
			typesToStream: map[string]source.Extra{pipelineResource: nil},
			expectErr:     true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(apiHandler)
			defer srv.Close()

			s := &Source{c: newTestGitLabClient(t, srv)}
			p := &pipelineEventProcessor{}

			data, err := p.process(t.Context(), s, tc.typesToStream, tc.body)
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
