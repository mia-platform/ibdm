// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func computeSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestGetWebhookMissingSecret(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookSecret: "",
			WebhookPath:   "/bitbucket/webhook",
		},
	}

	_, err := s.GetWebhook(t.Context(), nil, nil)
	require.ErrorIs(t, err, ErrBitbucketSource)
	require.ErrorIs(t, err, ErrMissingEnvVariable)
}

func TestGetWebhookValidConfig(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookSecret: "mysecret",
			WebhookPath:   "/bitbucket/webhook",
		},
	}

	webhook, err := s.GetWebhook(t.Context(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, webhook.Method)
	assert.Equal(t, "/bitbucket/webhook", webhook.Path)
	require.NotNil(t, webhook.Handler)
}

func TestWebhookHandlerMissingSignature(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookSecret: "mysecret",
			WebhookPath:   "/bitbucket/webhook",
		},
	}

	webhook, err := s.GetWebhook(t.Context(), nil, nil)
	require.NoError(t, err)

	headers := http.Header{}
	err = webhook.Handler(t.Context(), headers, []byte(`{}`))
	require.ErrorIs(t, err, ErrBitbucketSource)
	assert.Contains(t, err.Error(), "missing X-Hub-Signature")
}

func TestWebhookHandlerInvalidSignature(t *testing.T) {
	t.Parallel()

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookSecret: "mysecret",
			WebhookPath:   "/bitbucket/webhook",
		},
	}

	webhook, err := s.GetWebhook(t.Context(), nil, nil)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("X-Hub-Signature", "sha256=invalid")
	err = webhook.Handler(t.Context(), headers, []byte(`{}`))
	require.ErrorIs(t, err, ErrBitbucketSource)
	assert.Contains(t, err.Error(), "invalid webhook signature")
}

func TestWebhookHandlerValidSignatureKnownEvent(t *testing.T) {
	fixedTime := setupFixedTime(t)

	secret := "mysecret"
	body := []byte(`{"repository":{"full_name":"ws/repo1","slug":"repo1"}}`)
	signature := computeSignature(body, secret)

	// Set up a test server for enrichment
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"full_name":  "ws/repo1",
			"slug":       "repo1",
			"updated_on": "2025-01-10T14:22:33Z",
		})
	}))
	t.Cleanup(server.Close)

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookSecret: secret,
			WebhookPath:   "/bitbucket/webhook",
		},
		client: &client{
			baseURL:     server.URL,
			accessToken: "test-token",
			httpClient:  server.Client(),
		},
	}

	typesToStream := map[string]source.Extra{
		repositoryType: {},
	}

	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), typesToStream, results)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("X-Hub-Signature", signature)
	headers.Set(bitbucketEventHeader, repoPushEventKey)

	err = webhook.Handler(t.Context(), headers, body)
	require.NoError(t, err)

	select {
	case data := <-results:
		assert.Equal(t, repositoryType, data.Type)
		assert.Equal(t, source.DataOperationUpsert, data.Operation)
		repo := data.Values["repository"].(map[string]any)
		assert.Equal(t, "ws/repo1", repo["full_name"])
		_ = fixedTime
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for webhook result")
	}
}

func TestWebhookHandlerUnknownEvent(t *testing.T) {
	secret := "mysecret"
	body := []byte(`{"action":"ping"}`)
	signature := computeSignature(body, secret)

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookSecret: secret,
			WebhookPath:   "/bitbucket/webhook",
		},
	}

	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), nil, results)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("X-Hub-Signature", signature)
	headers.Set(bitbucketEventHeader, "unknown:event")

	err = webhook.Handler(t.Context(), headers, body)
	require.NoError(t, err)

	select {
	case d := <-results:
		t.Fatalf("unexpected data on results channel: %v", d)
	case <-time.After(100 * time.Millisecond):
		// no data within the window — correct
	}
}

func TestWebhookHandlerProcessorError(t *testing.T) {
	secret := "mysecret"
	body := []byte(`not json`)
	signature := computeSignature(body, secret)

	s := &Source{
		webhookConfig: webhookConfig{
			WebhookSecret: secret,
			WebhookPath:   "/bitbucket/webhook",
		},
		client: &client{},
	}

	typesToStream := map[string]source.Extra{
		repositoryType: {},
	}

	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), typesToStream, results)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("X-Hub-Signature", signature)
	headers.Set(bitbucketEventHeader, repoPushEventKey)

	err = webhook.Handler(t.Context(), headers, body)
	require.NoError(t, err)

	select {
	case d := <-results:
		t.Fatalf("unexpected data on results channel: %v", d)
	case <-time.After(100 * time.Millisecond):
		// processor failed → no data — correct
	}
}

func TestVerifySignature(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		body      []byte
		secret    string
		signature string
		expected  bool
	}{
		"valid signature": {
			body:      []byte("hello"),
			secret:    "secret",
			signature: computeSignature([]byte("hello"), "secret"),
			expected:  true,
		},
		"wrong secret": {
			body:      []byte("hello"),
			secret:    "wrong",
			signature: computeSignature([]byte("hello"), "secret"),
			expected:  false,
		},
		"missing sha256 prefix": {
			body:      []byte("hello"),
			secret:    "secret",
			signature: "noprefixhex",
			expected:  false,
		},
		"invalid hex": {
			body:      []byte("hello"),
			secret:    "secret",
			signature: "sha256=notvalidhex!!",
			expected:  false,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, verifySignature(tc.body, tc.signature, tc.secret))
		})
	}
}
