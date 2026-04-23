// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

// computeNexusSignature produces the HMAC-SHA1 hex digest used by Nexus webhooks.
func computeNexusSignature(body []byte, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestGetWebhookReturnsValidWebhook(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookPath: "/nexus/webhook",
		},
	}

	webhook, err := s.GetWebhook(t.Context(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, webhook.Method)
	assert.Equal(t, "/nexus/webhook", webhook.Path)
	require.NotNil(t, webhook.Handler)
}

func TestWebhookHandlerMissingSignatureWhenSecretConfigured(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookSecret: "mysecret",
			WebhookPath:   "/nexus/webhook",
		},
	}

	webhook, err := s.GetWebhook(t.Context(), nil, nil)
	require.NoError(t, err)

	headers := http.Header{}
	err = webhook.Handler(t.Context(), headers, []byte(`{}`))
	require.ErrorIs(t, err, ErrNexusSource)
	assert.Contains(t, err.Error(), "missing")
	assert.Contains(t, err.Error(), nexusSignatureHeader)
}

func TestWebhookHandlerInvalidSignature(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookSecret: "mysecret",
			WebhookPath:   "/nexus/webhook",
		},
	}

	webhook, err := s.GetWebhook(t.Context(), nil, nil)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set(nexusSignatureHeader, "deadbeefdeadbeef")
	err = webhook.Handler(t.Context(), headers, []byte(`{}`))
	require.ErrorIs(t, err, ErrNexusSource)
	assert.Contains(t, err.Error(), "invalid webhook signature")
}

func TestWebhookHandlerNoSecretAcceptsWithoutSignature(t *testing.T) {
	timeSource = func() time.Time { return testTime }

	apiComponent := map[string]any{
		"id":         "comp-api-id",
		"repository": "npm-proxy",
		"format":     "npm",
		"name":       "angular2",
		"version":    "0.0.2",
		"assets": []any{
			map[string]any{"id": "asset1", "path": "angular2/0.0.2.tgz"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiComponent)
	}))
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	s := &Source{
		config: config{URLHost: u.Host},
		webhookConfig: webhookConfig{
			WebhookPath: "/nexus/webhook",
		},
		client: &client{
			baseURL:       u,
			tokenName:     "tok",
			tokenPasscode: "pass",
			httpClient:    server.Client(),
		},
	}

	typesToStream := map[string]source.Extra{componentAssetType: {}}
	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), typesToStream, results)
	require.NoError(t, err)

	body := []byte(`{"action":"CREATED","repositoryName":"npm-proxy","component":{"id":"08909bf0","componentId":"comp-api-id","format":"npm","name":"angular2","group":"types","version":"0.0.2"}}`)
	headers := http.Header{}
	headers.Set(nexusEventHeader, componentEventKey)

	err = webhook.Handler(t.Context(), headers, body)
	require.NoError(t, err)

	select {
	case d := <-results:
		assert.Equal(t, componentAssetType, d.Type)
		assert.Equal(t, source.DataOperationUpsert, d.Operation)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for result")
	}
}

func TestWebhookHandlerValidSignatureKnownEventCreated(t *testing.T) {
	timeSource = func() time.Time { return testTime }

	secret := "mysecret"
	apiComponent := map[string]any{
		"id":         "comp-api-id",
		"repository": "npm-proxy",
		"format":     "npm",
		"name":       "angular2",
		"version":    "0.0.2",
		"assets": []any{
			map[string]any{"id": "asset1", "path": "angular2/0.0.2.tgz"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiComponent)
	}))
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	s := &Source{
		config: config{URLHost: u.Host},
		webhookConfig: webhookConfig{
			WebhookSecret: secret,
			WebhookPath:   "/nexus/webhook",
		},
		client: &client{
			baseURL:       u,
			tokenName:     "tok",
			tokenPasscode: "pass",
			httpClient:    server.Client(),
		},
	}

	typesToStream := map[string]source.Extra{componentAssetType: {}}
	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), typesToStream, results)
	require.NoError(t, err)

	body := []byte(`{"action":"CREATED","repositoryName":"npm-proxy","component":{"id":"08909bf0","componentId":"comp-api-id","format":"npm","name":"angular2","group":"types","version":"0.0.2"}}`)
	signature := computeNexusSignature(body, secret)

	headers := http.Header{}
	headers.Set(nexusSignatureHeader, signature)
	headers.Set(nexusEventHeader, componentEventKey)

	err = webhook.Handler(t.Context(), headers, body)
	require.NoError(t, err)

	select {
	case d := <-results:
		assert.Equal(t, componentAssetType, d.Type)
		assert.Equal(t, source.DataOperationUpsert, d.Operation)
		assert.Equal(t, testTime, d.Time)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for result")
	}
}

func TestWebhookHandlerValidSignatureKnownEventDeleted(t *testing.T) {
	timeSource = func() time.Time { return testTime }

	secret := "mysecret"
	body := []byte(`{"action":"DELETED","repositoryName":"npm-proxy","component":{"id":"08909bf0","componentId":"comp-del-id","format":"npm","name":"angular2","group":"types","version":"0.0.2"}}`)
	signature := computeNexusSignature(body, secret)

	s := &Source{
		config: config{URLHost: "nexus.example.com"},
		webhookConfig: webhookConfig{
			WebhookSecret: secret,
			WebhookPath:   "/nexus/webhook",
		},
		client: &client{},
	}

	typesToStream := map[string]source.Extra{componentAssetType: {}}
	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), typesToStream, results)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set(nexusSignatureHeader, signature)
	headers.Set(nexusEventHeader, componentEventKey)

	err = webhook.Handler(t.Context(), headers, body)
	require.NoError(t, err)

	select {
	case d := <-results:
		assert.Equal(t, componentAssetType, d.Type)
		assert.Equal(t, source.DataOperationDelete, d.Operation)
		assert.Equal(t, "comp-del-id", d.Values["id"])
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for result")
	}
}

func TestWebhookHandlerUnknownEventType(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookPath: "/nexus/webhook",
		},
	}

	results := make(chan source.Data, 10)
	webhook, err := s.GetWebhook(t.Context(), nil, results)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set(nexusEventHeader, "rm:audit:audit")
	err = webhook.Handler(t.Context(), headers, []byte(`{}`))
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, results)
}

func TestWebhookHandlerProcessorError(t *testing.T) {
	timeSource = func() time.Time { return testTime }

	s := &Source{
		config: config{URLHost: "nexus.example.com"},
		webhookConfig: webhookConfig{
			WebhookPath: "/nexus/webhook",
		},
		client: &client{},
	}

	typesToStream := map[string]source.Extra{componentAssetType: {}}
	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), typesToStream, results)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set(nexusEventHeader, componentEventKey)
	err = webhook.Handler(t.Context(), headers, []byte(`not json`))
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, results)
}

func TestVerifySignature(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	body := []byte(`{"test":"data"}`)
	validSig := computeNexusSignature(body, secret)

	tests := map[string]struct {
		signature string
		expect    bool
	}{
		"valid signature": {signature: validSig, expect: true},
		"wrong signature": {signature: computeNexusSignature(body, "wrong"), expect: false},
		"invalid hex":     {signature: "not-hex!!", expect: false},
		"empty signature": {signature: "", expect: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := verifySignature(body, tc.signature, secret)
			assert.Equal(t, tc.expect, result)
		})
	}
}
