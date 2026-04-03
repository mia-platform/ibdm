// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
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
		config: config{
			WebhookSecret: "",
			WebhookPath:   "/webhook/github",
		},
	}

	_, err := s.GetWebhook(t.Context(), nil, nil)
	require.ErrorIs(t, err, ErrGitHubSource)
	require.ErrorIs(t, err, ErrMissingEnvVariable)
}

func TestGetWebhookValidConfig(t *testing.T) {
	t.Parallel()

	s := &Source{
		config: config{
			WebhookSecret: "mysecret",
			WebhookPath:   "/webhook/github",
		},
	}

	webhook, err := s.GetWebhook(t.Context(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, webhook.Method)
	assert.Equal(t, "/webhook/github", webhook.Path)
	require.NotNil(t, webhook.Handler)
}

func TestWebhookHandlerMissingSignature(t *testing.T) {
	t.Parallel()

	s := &Source{
		config: config{
			WebhookSecret: "mysecret",
			WebhookPath:   "/webhook/github",
		},
	}

	webhook, err := s.GetWebhook(t.Context(), nil, nil)
	require.NoError(t, err)

	headers := http.Header{}
	err = webhook.Handler(t.Context(), headers, []byte(`{}`))
	require.ErrorIs(t, err, ErrGitHubSource)
	assert.Contains(t, err.Error(), "missing X-Hub-Signature-256")
}

func TestWebhookHandlerInvalidSignature(t *testing.T) {
	t.Parallel()

	s := &Source{
		config: config{
			WebhookSecret: "mysecret",
			WebhookPath:   "/webhook/github",
		},
	}

	webhook, err := s.GetWebhook(t.Context(), nil, nil)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", "sha256=invalid")
	err = webhook.Handler(t.Context(), headers, []byte(`{}`))
	require.ErrorIs(t, err, ErrGitHubSource)
	assert.Contains(t, err.Error(), "invalid webhook signature")
}

func TestWebhookHandlerValidSignatureKnownEvent(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	secret := "mysecret"
	body := []byte(`{"action":"created","repository":{"id":1,"name":"repo1"}}`)
	signature := computeSignature(body, secret)

	s := &Source{
		config: config{
			WebhookSecret: secret,
			WebhookPath:   "/webhook/github",
		},
	}

	typesToStream := map[string]source.Extra{
		repositoryType: {},
	}

	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), typesToStream, results)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", signature)
	headers.Set(githubEventHeader, repositoryEventHeaderValue)
	headers.Set("Content-Type", "application/json")

	err = webhook.Handler(t.Context(), headers, body)
	require.NoError(t, err)

	select {
	case data := <-results:
		assert.Equal(t, repositoryType, data.Type)
		assert.Equal(t, source.DataOperationUpsert, data.Operation)
		assert.Equal(t, map[string]any{repositoryType: map[string]any{"id": float64(1), "name": "repo1"}}, data.Values)
		assert.Equal(t, fixedTime, data.Time)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for webhook result")
	}
}

func TestWebhookHandlerUnknownEvent(t *testing.T) {
	secret := "mysecret"
	body := []byte(`{"action":"ping"}`)
	signature := computeSignature(body, secret)

	s := &Source{
		config: config{
			WebhookSecret: secret,
			WebhookPath:   "/webhook/github",
		},
	}

	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), nil, results)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", signature)
	headers.Set(githubEventHeader, "ping")
	headers.Set("Content-Type", "application/json")

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
		config: config{
			WebhookSecret: secret,
			WebhookPath:   "/webhook/github",
		},
	}

	typesToStream := map[string]source.Extra{
		repositoryType: {},
	}

	results := make(chan source.Data, 10)

	webhook, err := s.GetWebhook(t.Context(), typesToStream, results)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", signature)
	headers.Set(githubEventHeader, repositoryEventHeaderValue)
	headers.Set("Content-Type", "application/json")

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

func TestExtractJSONBody(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		contentType string
		body        string
		expected    []byte
	}{
		"application/json": {
			contentType: "application/json",
			body:        `{"key":"value"}`,
			expected:    []byte(`{"key":"value"}`),
		},
		"application/json with charset": {
			contentType: "application/json; charset=utf-8",
			body:        `{"key":"value"}`,
			expected:    []byte(`{"key":"value"}`),
		},
		"form urlencoded with payload": {
			contentType: "application/x-www-form-urlencoded",
			body:        `payload=%7B%22key%22%3A%22value%22%7D`,
			expected:    []byte(`{"key":"value"}`),
		},
		"form urlencoded missing payload": {
			contentType: "application/x-www-form-urlencoded",
			body:        `other=field`,
			expected:    nil,
		},
		"unsupported content type": {
			contentType: "text/plain",
			body:        "hello",
			expected:    nil,
		},
		"empty content type": {
			contentType: "",
			body:        "hello",
			expected:    nil,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			headers := http.Header{}
			headers.Set("Content-Type", tc.contentType)
			result := extractJSONBody(headers, []byte(tc.body))
			assert.Equal(t, tc.expected, result)
		})
	}
}
