// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestSource_GetWebhook(t *testing.T) {
	t.Run("successfully creates webhook and processes events", func(t *testing.T) {
		ctx := t.Context()
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		results := make(chan source.Data, 1)
		typesToStream := []string{BaseResourcePath + "Project"}

		webhook, err := s.GetWebhook(ctx, typesToStream, results)
		require.NoError(t, err)

		require.Equal(t, "/webhook", webhook.Path)
		require.Equal(t, http.MethodPost, webhook.Method)
		require.NotNil(t, webhook.Handler)

		payload := map[string]any{
			"eventName": "project_created",
			"data": map[string]any{
				"name": "test-project",
				"key":  "value",
			},
		}
		body, err := json.Marshal(payload)
		require.NoError(t, err)

		err = webhook.Handler(body)
		require.NoError(t, err)

		select {
		case data := <-results:
			require.Equal(t, BaseResourcePath+"Project", data.Type)
			require.Equal(t, source.DataOperationUpsert, data.Operation)
			require.Equal(t, payload["data"], data.Values)
		default:
			t.Fatal("expected data in channel")
		}
	})

	t.Run("ignores events not in typesToStream", func(t *testing.T) {
		ctx := t.Context()
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		results := make(chan source.Data, 1)
		typesToStream := []string{BaseResourcePath + "Project"}

		webhook, err := s.GetWebhook(ctx, typesToStream, results)
		require.NoError(t, err)

		payload := map[string]any{
			"eventName": "order_created",
			"data": map[string]any{
				"name": "test-order",
				"key":  "value",
			},
		}
		body, err := json.Marshal(payload)
		require.NoError(t, err)

		err = webhook.Handler(body)
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

		s, err := NewSource()
		require.NoError(t, err)

		results := make(chan source.Data, 1)
		typesToStream := []string{"users"}

		webhook, err := s.GetWebhook(ctx, typesToStream, results)
		require.NoError(t, err)

		err = webhook.Handler([]byte(`{invalid-json`))
		require.Error(t, err)
	})
}

func TestNewSource(t *testing.T) {
	t.Run("creates source successfully", func(t *testing.T) {
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")
		s, err := NewSource()
		require.NoError(t, err)
		require.NotNil(t, s)
		require.NotNil(t, s.c)
		require.Equal(t, "/webhook", s.c.config.WebhookPath)
	})
}
