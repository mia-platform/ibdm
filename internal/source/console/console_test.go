// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
	"github.com/mia-platform/ibdm/internal/source/console/service"
)

func TestSource_NewSource(t *testing.T) {
	t.Run("fails when CONSOLE_WEBHOOK_PATH is missing", func(t *testing.T) {
		s, err := NewSource()
		require.Error(t, err)
		require.Nil(t, s)
	})

	t.Run("succeeds with valid config", func(t *testing.T) {
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")
		t.Setenv("CONSOLE_ENDPOINT", "http://example.com")
		s, err := NewSource()
		require.NoError(t, err)
		require.NotNil(t, s)
		require.Equal(t, "/webhook", s.c.config.WebhookPath)
	})
}

func TestSource_GetWebhook(t *testing.T) {
	t.Parallel()
	t.Run("successfully creates webhook and processes events", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		s := Source{
			c: &webhookClient{
				config: webhookConfig{
					WebhookPath: "/webhook",
				},
			},
		}

		results := make(chan source.Data, 1)
		typesToStream := map[string]source.Extra{"project": {}}

		webhook, err := s.GetWebhook(ctx, typesToStream, results)
		require.NoError(t, err)

		require.Equal(t, "/webhook", webhook.Path)
		require.Equal(t, http.MethodPost, webhook.Method)
		require.NotNil(t, webhook.Handler)

		payload := map[string]any{
			"eventName": "project_created",
			"payload": map[string]any{
				"name": "test-project",
				"key":  "value",
			},
		}
		body, err := json.Marshal(payload)
		require.NoError(t, err)

		headers := http.Header{}
		err = webhook.Handler(headers, body)
		require.NoError(t, err)

		expectedEvent := event{
			EventName: "project_created",
			Payload: map[string]any{
				"name": "test-project",
				"key":  "value",
			},
		}

		select {
		case data := <-results:
			require.Equal(t, expectedEvent.GetResource(), data.Type)
			require.Equal(t, expectedEvent.Operation(), data.Operation)
			require.Equal(t, expectedEvent.Payload, data.Values)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for message processing: expected data in channel")
		}
	})

	t.Run("ignores events not in typesToStream", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		s := Source{
			c: &webhookClient{
				config: webhookConfig{
					WebhookPath: "/webhook",
				},
			},
		}

		results := make(chan source.Data, 1)
		typesToStream := map[string]source.Extra{"project": {}}

		webhook, err := s.GetWebhook(ctx, typesToStream, results)
		require.NoError(t, err)

		payload := map[string]any{
			"eventName": "order_created",
			"payload": map[string]any{
				"name": "test-order",
				"key":  "value",
			},
		}
		body, err := json.Marshal(payload)
		require.NoError(t, err)

		headers := http.Header{}
		err = webhook.Handler(headers, body)
		require.NoError(t, err)

		select {
		case <-results:
			t.Fatal("did not expect data in channel")
		default:
		}
	})

	t.Run("returns error on invalid json", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		s := Source{
			c: &webhookClient{
				config: webhookConfig{
					WebhookPath: "/webhook",
				},
			},
		}

		results := make(chan source.Data, 1)
		typesToStream := map[string]source.Extra{"user": {}}

		webhook, err := s.GetWebhook(ctx, typesToStream, results)
		require.NoError(t, err)

		body := []byte(`{invalid-json`)
		headers := http.Header{}
		err = webhook.Handler(headers, body)
		require.Error(t, err)
	})
}

func Test_DoChain(t *testing.T) {
	tests := map[string]struct {
		event         event
		handler       http.HandlerFunc
		expectedError error
		expectedData  []source.Data
	}{
		"configuration event": {
			event: event{
				EventName:      "configuration_created",
				EventTimestamp: 1672531200000, // 2023-01-01 00:00:00 UTC
				Payload: map[string]any{
					"projectId":    "p1",
					"revisionName": "r1",
				},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/projects/p1/revisions/r1/configuration" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"key": "value"})
			},
			expectedData: []source.Data{
				{
					Type:      "configuration",
					Operation: source.DataOperationUpsert,
					Values: map[string]any{
						"project": map[string]any{
							"_id":      "p1",
							"tenantId": "",
						},
						"revision": map[string]any{
							"name": "r1",
						},
						"configuration": map[string]any{
							"key": "value",
						},
					},
					Time: time.Unix(1672531200, 0),
				},
			},
		},
		"project event: delete": {
			event: event{
				EventName:      "project_deleted",
				EventTimestamp: 1672531200000,
				Payload: map[string]any{
					"id": "123",
				},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotImplemented)
			},
			expectedData: []source.Data{
				{
					Type:      "project",
					Operation: source.DataOperationDelete,
					Values: map[string]any{
						"id": "123",
					},
					Time: time.Unix(1672531200, 0),
				},
			},
		},
		"other event": {
			event: event{
				EventName:      "other_resource_updated",
				EventTimestamp: 1672531200000,
				Payload: map[string]any{
					"foo": "bar",
				},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotImplemented)
			},
			expectedData: []source.Data{
				{
					Type:      "other_resource",
					Operation: source.DataOperationUpsert,
					Values: map[string]any{
						"foo": "bar",
					},
					Time: time.Unix(1672531200, 0),
				},
			},
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			ctx := t.Context()

			server := httptest.NewServer(test.handler)
			defer server.Close()
			t.Setenv("CONSOLE_ENDPOINT", server.URL)

			cs, err := service.NewConsoleService()
			require.NoError(t, err)

			ch := make(chan source.Data, len(test.expectedData)+1)

			err = doChain(ctx, test.event, ch, cs)
			if test.expectedError != nil {
				require.ErrorIs(t, err, test.expectedError)
				return
			}
			require.NoError(t, err)
			close(ch)

			var data []source.Data
			for d := range ch {
				if d.Type == "configuration" {
					d.Time = time.Time{}
				}
				data = append(data, d)
			}

			expected := make([]source.Data, len(test.expectedData))
			copy(expected, test.expectedData)
			for i := range expected {
				if expected[i].Type == "configuration" {
					expected[i].Time = time.Time{}
				}
			}

			require.ElementsMatch(t, expected, data)
		})
	}
}

func TestSource_listAssets(t *testing.T) {
	t.Run("successfully lists projects and configurations", func(t *testing.T) {
		ctx := t.Context()

		project1 := map[string]any{
			"_id":       "p1",
			"projectId": "project-1",
			"tenantId":  "tenant-1",
		}

		revision1 := map[string]any{
			"name": "r1",
		}

		handler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/projects/":
				json.NewEncoder(w).Encode([]map[string]any{project1})
			case "/projects/p1/revisions":
				json.NewEncoder(w).Encode([]map[string]any{revision1})
			case "/projects/p1/revisions/r1/configuration":
				json.NewEncoder(w).Encode(map[string]any{
					"key": "value",
					"fastDataConfig": map[string]any{
						"castFunctions": "some-code",
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}

		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		typesToSync := map[string]source.Extra{
			"project":       {},
			"configuration": {},
		}

		data, err := s.listAssets(ctx, typesToSync)
		require.NoError(t, err)
		require.Len(t, data, 2)

		projectData := data[0]
		require.Equal(t, "project", projectData.Type)
		require.Equal(t, source.DataOperationUpsert, projectData.Operation)
		require.WithinDuration(t, time.Now(), projectData.Time, 5*time.Second)

		expectedProjectValues := map[string]any{
			"project": map[string]any{
				"_id":       "p1",
				"projectId": "project-1",
				"tenantId":  "tenant-1",
			},
		}
		require.Equal(t, expectedProjectValues, projectData.Values)

		configData := data[1]
		require.Equal(t, "configuration", configData.Type)
		require.Equal(t, source.DataOperationUpsert, configData.Operation)
		require.WithinDuration(t, time.Now(), configData.Time, 5*time.Second)

		expectedConfigValues := map[string]any{
			"project": map[string]any{
				"_id":       "p1",
				"projectId": "project-1",
				"tenantId":  "tenant-1",
			},
			"revision": map[string]any{
				"name": "r1",
			},
			"configuration": map[string]any{
				"key": "value",
				"fastDataConfig": map[string]any{
					"castFunctions": nil,
				},
			},
		}
		require.Equal(t, expectedConfigValues, configData.Values)
	})

	t.Run("returns error when GetProjects fails", func(t *testing.T) {
		ctx := t.Context()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		typesToSync := map[string]source.Extra{"project": {}}

		_, err = s.listAssets(ctx, typesToSync)
		require.ErrorIs(t, err, ErrRetrievingAssets)
	})

	t.Run("returns error when GetRevisions fails during configuration sync", func(t *testing.T) {
		ctx := t.Context()

		handler := func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/projects/" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]map[string]any{{"_id": "p1"}})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}

		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		typesToSync := map[string]source.Extra{"configuration": {}}

		_, err = s.listAssets(ctx, typesToSync)
		require.ErrorIs(t, err, ErrRetrievingAssets)
	})

	t.Run("returns error when GetConfiguration fails during configuration sync", func(t *testing.T) {
		ctx := t.Context()

		handler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/projects/":
				json.NewEncoder(w).Encode([]map[string]any{{"_id": "p1"}})
			case "/projects/p1/revisions":
				json.NewEncoder(w).Encode([]map[string]any{{"name": "r1"}})
			default:
				w.WriteHeader(http.StatusInternalServerError)
			}
		}

		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		typesToSync := map[string]source.Extra{"configuration": {}}

		_, err = s.listAssets(ctx, typesToSync)
		require.ErrorIs(t, err, ErrRetrievingAssets)
	})
}
