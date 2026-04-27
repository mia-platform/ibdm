// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

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

func newTestSource(t *testing.T, handler http.Handler, specificRepository string) *Source {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	return &Source{
		config: config{
			URLSchema:          u.Scheme,
			URLHost:            u.Host,
			TokenName:          "test-token",
			TokenPasscode:      "test-passcode",
			HTTPTimeout:        5 * time.Second,
			SpecificRepository: specificRepository,
		},
		webhookConfig: webhookConfig{
			WebhookPath: "/nexus/webhook",
		},
		client: &client{
			baseURL:       u,
			tokenName:     "test-token",
			tokenPasscode: "test-passcode",
			httpClient: &http.Client{
				Timeout: 5 * time.Second,
			},
		},
	}
}

func collectData(t *testing.T, ch <-chan source.Data) []source.Data {
	t.Helper()
	var result []source.Data
	for d := range ch {
		result = append(result, d)
	}
	return result
}
