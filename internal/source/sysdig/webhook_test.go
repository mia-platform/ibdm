// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sysdig

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestGetWebhookMissingBaseURL(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookPath: "/sysdig/webhook",
			BearerToken: "test-token",
		},
	}

	_, err := s.GetWebhook(t.Context(), nil, nil)
	require.ErrorIs(t, err, ErrSysdigSource)
	require.ErrorIs(t, err, ErrMissingEnvVariable)
	assert.Contains(t, err.Error(), "SYSDIG_BASE_URL")
}

func TestGetWebhookMissingBearerToken(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookPath: "/sysdig/webhook",
			BaseURL:     "https://eu1.app.sysdig.com",
		},
	}

	_, err := s.GetWebhook(t.Context(), nil, nil)
	require.ErrorIs(t, err, ErrSysdigSource)
	require.ErrorIs(t, err, ErrMissingEnvVariable)
	assert.Contains(t, err.Error(), "SYSDIG_BEARER_TOKEN")
}

func TestGetWebhookValidConfig(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookPath: "/sysdig/webhook",
			BaseURL:     "https://eu1.app.sysdig.com",
			BearerToken: "test-token",
		},
	}

	webhook, err := s.GetWebhook(t.Context(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, webhook.Method)
	assert.Equal(t, "/sysdig/webhook", webhook.Path)
	require.NotNil(t, webhook.Handler)
}

func TestWebhookHandlerInvalidJSON(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookPath: "/sysdig/webhook",
			BaseURL:     "https://eu1.app.sysdig.com",
			BearerToken: "test-token",
		},
	}

	webhook, err := s.GetWebhook(t.Context(), nil, nil)
	require.NoError(t, err)

	err = webhook.Handler(t.Context(), http.Header{}, []byte("not json"))
	require.ErrorIs(t, err, ErrSysdigSource)
}

func TestWebhookHandlerUnknownEvent(t *testing.T) {
	s := &Source{
		webhookConfig: webhookConfig{
			WebhookPath: "/sysdig/webhook",
			BaseURL:     "https://eu1.app.sysdig.com",
			BearerToken: "test-token",
		},
	}

	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), nil, results)
	require.NoError(t, err)

	body, err := json.Marshal(webhookPayload{
		Event: webhookEvent{
			ID:        "Some Other Alert",
			EventData: webhookEventData{Name: "Some Other Alert"},
		},
		Timestamp: 1777471695260063,
	})
	require.NoError(t, err)

	err = webhook.Handler(t.Context(), http.Header{}, body)
	require.NoError(t, err)

	// No data should be produced for an unknown event.
	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, results)
}

func TestWebhookHandlerValidEventViaEventID(t *testing.T) {
	resultID := "18aad9169a529683f1a57da4199aeb00"
	eventURL := fmt.Sprintf("https://eu1.app.sysdig.com/secure/#/vulnerabilities/results/%s/overview", resultID)
	timestampMicros := int64(1777471695260063)
	expectedTime := time.UnixMicro(timestampMicros)

	apiResponse := resultResponse{
		Result: resultData{
			Type: "dockerImage",
			Metadata: resultMetadata{
				PullString: "registry.example.com/my-service:v2.0.0",
			},
			Packages: []resultPackage{
				{
					Vulns: []map[string]any{
						{
							"name": "CVE-2025-99999",
							"severity": map[string]any{
								"value":      "High",
								"sourceName": "nvd",
							},
						},
					},
				},
			},
			PolicyEvaluationsResult: "failed",
		},
	}

	vc := newTestVulnerabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, resultID)
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(apiResponse)
		require.NoError(t, err)
	}))

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookPath: "/sysdig/webhook",
			BaseURL:     vc.baseURL,
			BearerToken: vc.bearerToken,
		},
		vulnClient: vc,
	}

	typesToStream := map[string]source.Extra{
		vulnerabilityType: {},
	}

	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), typesToStream, results)
	require.NoError(t, err)

	body, err := json.Marshal(webhookPayload{
		Event: webhookEvent{
			ID:  pipelineFailureAlertKey,
			URL: eventURL,
		},
		Timestamp: timestampMicros,
	})
	require.NoError(t, err)

	err = webhook.Handler(t.Context(), http.Header{}, body)
	require.NoError(t, err)

	select {
	case data := <-results:
		assert.Equal(t, vulnerabilityType, data.Type)
		assert.Equal(t, source.DataOperationUpsert, data.Operation)
		assert.Equal(t, expectedTime, data.Time)

		img, ok := data.Values["img"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "registry.example.com/my-service:v2.0.0", img["imageReference"])

		vuln, ok := data.Values["vuln"].(map[string]any)
		require.True(t, ok, "vuln must be map[string]any")
		assert.Equal(t, "CVE-2025-99999", vuln["name"])
		assert.Equal(t, "High", vuln["severity"])
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for webhook result")
	}
}

func TestWebhookHandlerValidEventViaEventDataName(t *testing.T) {
	resultID := "abcdef1234567890abcdef1234567890"
	eventURL := fmt.Sprintf("https://app.sysdigcloud.com/secure/#/vulnerabilities/results/%s/overview", resultID)
	timestampMicros := int64(1777471695260063)

	apiResponse := resultResponse{
		Result: resultData{
			Type: "dockerImage",
			Metadata: resultMetadata{
				PullString: "registry.example.com/my-app:latest",
			},
			Packages: []resultPackage{
				{
					Vulns: []map[string]any{
						{"name": "CVE-2026-00001"},
					},
				},
			},
			PolicyEvaluationsResult: "failed",
		},
	}

	vc := newTestVulnerabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(apiResponse)
		require.NoError(t, err)
	}))

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookPath: "/sysdig/webhook",
			BaseURL:     vc.baseURL,
			BearerToken: vc.bearerToken,
		},
		vulnClient: vc,
	}

	results := make(chan source.Data, 10)
	typesToStream := map[string]source.Extra{vulnerabilityType: {}}

	webhook, err := s.GetWebhook(t.Context(), typesToStream, results)
	require.NoError(t, err)

	// event.id is different, but event.eventData.name matches
	body, err := json.Marshal(webhookPayload{
		Event: webhookEvent{
			ID:        "Other ID",
			URL:       eventURL,
			EventData: webhookEventData{Name: pipelineFailureAlertKey},
		},
		Timestamp: timestampMicros,
	})
	require.NoError(t, err)

	err = webhook.Handler(t.Context(), http.Header{}, body)
	require.NoError(t, err)

	select {
	case data := <-results:
		assert.Equal(t, vulnerabilityType, data.Type)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for webhook result")
	}
}

func TestWebhookHandlerProcessorError(t *testing.T) {
	resultID := "errorresultid1234567890abcdef1234"
	eventURL := fmt.Sprintf("https://eu1.app.sysdig.com/secure/#/vulnerabilities/results/%s/overview", resultID)

	vc := newTestVulnerabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte("internal error"))
		require.NoError(t, err)
	}))

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookPath: "/sysdig/webhook",
			BaseURL:     vc.baseURL,
			BearerToken: vc.bearerToken,
		},
		vulnClient: vc,
	}

	typesToStream := map[string]source.Extra{vulnerabilityType: {}}
	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), typesToStream, results)
	require.NoError(t, err)

	body, err := json.Marshal(webhookPayload{
		Event: webhookEvent{
			ID:  pipelineFailureAlertKey,
			URL: eventURL,
		},
		Timestamp: 1777471695260063,
	})
	require.NoError(t, err)

	err = webhook.Handler(t.Context(), http.Header{}, body)
	require.NoError(t, err)

	// Processor error is async — no data should arrive.
	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, results)
}

func TestResolveEventType(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body      []byte
		expectKey string
		expectErr bool
	}{
		"event.id matches known processor": {
			body:      mustMarshal(t, webhookPayload{Event: webhookEvent{ID: pipelineFailureAlertKey}}),
			expectKey: pipelineFailureAlertKey,
		},
		"event.eventData.name matches known processor": {
			body: mustMarshal(t, webhookPayload{
				Event: webhookEvent{
					ID:        "Other",
					EventData: webhookEventData{Name: pipelineFailureAlertKey},
				},
			}),
			expectKey: pipelineFailureAlertKey,
		},
		"neither matches returns event.id": {
			body: mustMarshal(t, webhookPayload{
				Event: webhookEvent{
					ID:        "Unknown Alert",
					EventData: webhookEventData{Name: "Unknown Alert"},
				},
			}),
			expectKey: "Unknown Alert",
		},
		"empty event returns empty string": {
			body:      mustMarshal(t, webhookPayload{}),
			expectKey: "",
		},
		"invalid JSON": {
			body:      []byte("not json"),
			expectErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			key, err := resolveEventType(tc.body)
			if tc.expectErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectKey, key)
		})
	}
}
