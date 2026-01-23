// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

type MockConsoleService struct {
	GetRevisionFunc func(ctx context.Context, projectId, resourceId string) (map[string]any, error)
	GetProjectsFunc func(ctx context.Context) ([]map[string]any, error)
}

func (m *MockConsoleService) GetProjects(ctx context.Context) ([]map[string]any, error) {
	return nil, nil
}

func (m *MockConsoleService) GetRevision(ctx context.Context, projectID, resourceID string) (map[string]any, error) {
	if m.GetRevisionFunc != nil {
		return m.GetRevisionFunc(ctx, projectID, resourceID)
	}
	return nil, nil
}

func TestSource_GetWebhook(t *testing.T) {
	t.Run("successfully creates webhook and processes events", func(t *testing.T) {
		ctx := t.Context()
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")
		t.Setenv("CONSOLE_ENDPOINT", "http://example.com")

		s, err := NewSource()
		require.NoError(t, err)

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

		// wait for data to be processed
		time.Sleep(1 * time.Second)

		select {
		case data := <-results:
			require.Equal(t, expectedEvent.GetResource(), data.Type)
			require.Equal(t, expectedEvent.Operation(), data.Operation)
			require.Equal(t, expectedEvent.Payload, data.Values)
		default:
			t.Fatal("expected data in channel")
		}
	})

	t.Run("ignores events not in typesToStream", func(t *testing.T) {
		ctx := t.Context()
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")
		t.Setenv("CONSOLE_ENDPOINT", "http://example.com")

		s, err := NewSource()
		require.NoError(t, err)

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
		ctx := t.Context()
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")
		t.Setenv("CONSOLE_ENDPOINT", "http://example.com")

		s, err := NewSource()
		require.NoError(t, err)

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

func TestNewSource(t *testing.T) {
	t.Run("creates source successfully", func(t *testing.T) {
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")
		t.Setenv("CONSOLE_ENDPOINT", "http://example.com")
		t.Setenv("CONSOLE_WEBHOOK_SECRET", "secret")
		t.Setenv("CONSOLE_PROJECT_ID", "test-project")
		s, err := NewSource()
		require.NoError(t, err)
		require.NotNil(t, s)
		require.NotNil(t, s.c)
		require.Equal(t, "/webhook", s.c.config.WebhookPath)
	})
}

func Test_DoChain(t *testing.T) {
	tests := map[string]struct {
		event         event
		mockSetup     func(*MockConsoleService)
		expectedError error
		expectedData  []source.Data
	}{
		"configuration event": {
			event: event{
				EventName:      "configuration_created",
				EventTimestamp: 1672531200, // 2023-01-01 00:00:00 UTC
				Payload: map[string]any{
					"projectId":  "p1",
					"resourceId": "r1",
				},
			},
			mockSetup: func(m *MockConsoleService) {
				m.GetRevisionFunc = func(ctx context.Context, projectId, resourceId string) (map[string]any, error) {
					if projectId == "p1" && resourceId == "r1" {
						return map[string]any{"key": "value"}, nil
					}
					return nil, nil
				}
			},
			expectedData: []source.Data{
				{
					Type:      "configuration",
					Operation: source.DataOperationUpsert,
					Values: map[string]any{
						"event": map[string]any{
							"projectId":  "p1",
							"resourceId": "r1",
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
				EventTimestamp: 1672531200,
				Payload: map[string]any{
					"id": "123",
				},
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
				EventTimestamp: 1672531200,
				Payload: map[string]any{
					"foo": "bar",
				},
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

			ch := make(chan source.Data, len(test.expectedData)+1)
			mockSvc := &MockConsoleService{}
			if test.mockSetup != nil {
				test.mockSetup(mockSvc)
			}

			err := doChain(ctx, test.event, ch, mockSvc)
			if test.expectedError != nil {
				require.ErrorIs(t, err, test.expectedError)
				return
			}
			require.NoError(t, err)
			close(ch)

			var data []source.Data
			for d := range ch {
				data = append(data, d)
			}
			require.ElementsMatch(t, test.expectedData, data)
		})
	}
}
