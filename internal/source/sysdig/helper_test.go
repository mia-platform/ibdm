// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sysdig

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestVulnerabilityClient(t *testing.T, handler http.Handler) *vulnerabilityClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &vulnerabilityClient{
		baseURL:     server.URL,
		bearerToken: "test-bearer-token",
		httpClient:  &http.Client{Timeout: 5 * time.Second},
	}
}
