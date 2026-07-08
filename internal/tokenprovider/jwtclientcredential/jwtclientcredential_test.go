// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jwtclientcredential

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

const rsaKeyBits = 4096

// generateTestKey creates a fresh RSA key pair and wraps it into a jwk.Key, to be used as
// fictional test material. It is never used outside of this test file.
func generateTestKey(t *testing.T) (*rsa.PrivateKey, jwk.Key) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, rsaKeyBits)
	require.NoError(t, err)

	jwkKey, err := jwk.Import(key)
	require.NoError(t, err)

	return key, jwkKey
}

func TestPrivateKeyFlow(t *testing.T) {
	t.Parallel()

	key, jwkKey := generateTestKey(t)
	const clientID = "jwt-bearer-client"

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "client_credentials", r.FormValue("grant_type"))
			assert.Equal(t, "urn:ietf:params:oauth:client-assertion-type:jwt-bearer", r.FormValue("client_assertion_type"))
			assert.Equal(t, clientID, r.FormValue("client_id"))
			assert.Equal(t, "private_key_jwt", r.FormValue("token_endpoint_auth_method"))

			assertion := r.FormValue("client_assertion")
			require.NotEmpty(t, assertion)

			parsed, err := jwt.Parse([]byte(assertion), jwt.WithKey(jwa.RS256(), key.Public()))
			require.NoError(t, err)

			issuer, ok := parsed.Issuer()
			require.True(t, ok)
			assert.Equal(t, clientID, issuer)

			subject, ok := parsed.Subject()
			require.True(t, ok)
			assert.Equal(t, clientID, subject)

			audience, ok := parsed.Audience()
			require.True(t, ok)
			assert.Equal(t, []string{"console-client-credentials"}, audience)

			jti, ok := parsed.JwtID()
			require.True(t, ok)
			assert.NotEmpty(t, jti)

			w.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(w).Encode(map[string]any{
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

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: NewProvider(t.Context(), clientID, testServer.URL+"/oauth/token", jwkKey),
		},
	}

	resp, err := client.Get(testServer.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestPrivateKeyFlowTokenEndpointError(t *testing.T) {
	t.Parallel()

	_, jwkKey := generateTestKey(t)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer testServer.Close()

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: NewProvider(t.Context(), "client-id", testServer.URL, jwkKey),
		},
	}

	resp, err := client.Get(testServer.URL + "/")
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenExchange)
	assert.ErrorContains(t, err, "upstream token exchange failed")
	assert.ErrorContains(t, err, "boom")
}

func TestPrivateKeyFlowMalformedTokenResponse(t *testing.T) {
	t.Parallel()

	_, jwkKey := generateTestKey(t)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("not-json"))
		require.NoError(t, err)
	}))
	defer testServer.Close()

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: NewProvider(t.Context(), "client-id", testServer.URL, jwkKey),
		},
	}

	resp, err := client.Get(testServer.URL + "/")
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenExchange)
	assert.ErrorContains(t, err, "failed to decode token response")
}
