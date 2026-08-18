// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package easm

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

var testTime = time.Date(2025, time.March, 1, 12, 0, 0, 0, time.UTC)

func init() {
	timeSource = func() time.Time {
		return testTime
	}
}

// newTestSource builds a Source whose client points at an httptest server
// running the given handler. The server is closed automatically when the test
// finishes.
func newTestSource(t *testing.T, handler http.Handler) *Source {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	cfg := config{
		BaseURL:     server.URL,
		DataPath:    "/data",
		Customer:    "acme",
		Token:       "test-token",
		HTTPTimeout: 5 * time.Second,
	}

	return &Source{
		config: cfg,
		client: &client{
			baseURL:  u,
			dataPath: cfg.DataPath,
			customer: cfg.Customer,
			token:    cfg.Token,
			httpClient: &http.Client{
				Timeout: cfg.HTTPTimeout,
			},
		},
	}
}

// collectData drains a source.Data channel into a slice.
func collectData(t *testing.T, ch <-chan source.Data) []source.Data {
	t.Helper()

	var result []source.Data
	for d := range ch {
		result = append(result, d)
	}
	return result
}
