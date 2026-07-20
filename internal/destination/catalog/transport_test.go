// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTransportWithoutCredentials(t *testing.T) {
	t.Parallel()

	transport, err := NewTransport(t.Context(), "", "", "", "", "", "", "", nil)
	assert.NoError(t, err)
	assert.Same(t, http.DefaultTransport, transport)
}

// TestNewTransportPrivateKeyJWTWiring verifies that NewTransport wires the client-ID/private-key
// case to an oauth2source provider by exercising a full token exchange plus authenticated
// request through the resulting transport. The JWT assertion contents and provider-level failure
// modes are covered by the oauth2source package's own tests.
func TestNewTransportPrivateKeyJWTWiring(t *testing.T) {
	t.Parallel()

	key := generateTestRSAKey(t)
	const clientID = "jwt-bearer-client"

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, clientID, r.FormValue("client_id"))
			assert.Equal(t, "organization:*", r.FormValue("scope"))

			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]any{
				"access_token": "generated-jwt-bearer-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			require.NoError(t, err)
		case "/":
			assert.Equal(t, "Bearer generated-jwt-bearer-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	transport, err := NewTransport(t.Context(), "", clientID, "", "", "", testServer.URL+"/oauth/token", "organization:*", newTestPrivateKeyFor(t, key))
	require.NoError(t, err)

	client := &http.Client{
		Transport: transport,
	}

	resp, err := client.Get(testServer.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}
