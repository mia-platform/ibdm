// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	webhooksDir = "testdata/webhooks"

	// webhookTimeout bounds the wait for the items a webhook produces: ibdm answers the webhook
	// before it processes the event.
	webhookTimeout = 10 * time.Second
	// webhookGracePeriod is how long a test waits before asserting that an event produced nothing.
	webhookGracePeriod = time.Second

	webhookSecret = "test-webhook-secret"
)

// webhookPayload reads a webhook payload fixture of integration.
func webhookPayload(t *testing.T, integration, name string) []byte {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(webhooksDir, integration, name))
	require.NoError(t, err)
	return body
}

// postWebhook posts body to path on the server proc with header, and returns the status.
func postWebhook(t *testing.T, proc *process, path string, header http.Header, body []byte) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, proc.baseURL+path, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header = header.Clone()
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	return resp.StatusCode
}

// githubSignature signs body as GitHub does: HMAC-SHA256 with the secret, hex encoded.
func githubSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// consoleSignature signs body as the Mia-Platform Console does: a plain SHA-256 of the body
// followed by the secret, hex encoded. It is not an HMAC.
func consoleSignature(body []byte, secret string) string {
	hash := sha256.New()
	hash.Write(body)
	hash.Write([]byte(secret))
	return hex.EncodeToString(hash.Sum(nil))
}

// assertNothingArrives waits for the grace period, then asserts that the Catalog received
// nothing. Webhook processing is asynchronous, so an immediate check would prove nothing.
func assertNothingArrives(t *testing.T, catalog *fakeCatalog) {
	t.Helper()

	time.Sleep(webhookGracePeriod)
	assert.Empty(t, catalog.received())
}
