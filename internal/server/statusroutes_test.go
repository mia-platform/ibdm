// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/info"
)

func TestStatusRoutes(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)
	expectedResponse := fmt.Sprintf(`{"status":"OK","name":"%s","version":"%s"}`, serviceName, info.Version)

	for _, path := range []string{"/-/healthz", "/-/ready", "/-/check-up"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+" "+path, func(t *testing.T) {
				t.Parallel()

				response := serve(srv, httptest.NewRequestWithContext(t.Context(), method, path, nil))

				require.Equal(t, http.StatusOK, response.Code)
				require.Equal(t, "application/json", response.Header().Get("Content-Type"))
				if method == http.MethodGet {
					require.Equal(t, expectedResponse, response.Body.String())
				}
			})
		}
	}

	t.Run("wrong method", func(t *testing.T) {
		t.Parallel()

		response := serve(srv, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/-/healthz", strings.NewReader("{}")))
		require.Equal(t, http.StatusMethodNotAllowed, response.Code)
	})
}

func TestStatusRoutesAreNotLogged(t *testing.T) {
	t.Parallel()

	srv, logs := newTestServer(t)
	for _, path := range []string{"/-/healthz", "/-/ready", "/-/check-up"} {
		require.Equal(t, http.StatusOK, serve(srv, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)).Code)
	}

	require.Empty(t, logs.String())
}
