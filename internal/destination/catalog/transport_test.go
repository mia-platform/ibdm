// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rsaKeyBits = 4096

// generateTestRSAKey creates a fresh RSA key pair to be used as fictional test material. It is
// never used outside of this test file.
func generateTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, rsaKeyBits)
	require.NoError(t, err)
	return key
}

// encodePKCS8PEM PEM-encodes key using the PKCS8 container, matching the format most identity
// providers expect for a "private key" credential.
func encodePKCS8PEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()

	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	}
	return string(pem.EncodeToMemory(block))
}

// encodeJWKJSON serializes key as a raw JWK JSON document.
func encodeJWKJSON(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()

	jwkKey, err := jwk.Import(key)
	require.NoError(t, err)

	raw, err := json.Marshal(jwkKey)
	require.NoError(t, err)
	return string(raw)
}

func TestNewTransportWithoutCredentials(t *testing.T) {
	t.Parallel()

	transport := NewTransport(t.Context(), "", "", "", "", "")
	assert.Same(t, http.DefaultTransport, transport)
}

func TestPrivateKeyFlow(t *testing.T) {
	t.Parallel()

	key := generateTestRSAKey(t)

	testCases := map[string]struct {
		usePEM bool
	}{
		"PEM encoded key": {
			usePEM: true,
		},
		"raw JWK JSON key": {
			usePEM: false,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			const clientID = "jwt-bearer-client"

			privateKey := encodeJWKJSON(t, key)
			if tc.usePEM {
				privateKey = encodePKCS8PEM(t, key)
			}

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
				Transport: NewTransport(t.Context(), "", testServer.URL+"/oauth/token", clientID, "", privateKey),
			}

			resp, err := client.Get(testServer.URL + "/")
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		})
	}
}

func TestPrivateKeyFlowInvalidKey(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: NewTransport(t.Context(), "", "http://unused.invalid/oauth/token", "client-id", "", "not-a-valid-key"),
	}

	resp, err := client.Get("http://unused.invalid/")
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorContains(t, err, "jwk initialization failed")
}

func TestPrivateKeyFlowTokenEndpointError(t *testing.T) {
	t.Parallel()

	key := generateTestRSAKey(t)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer testServer.Close()

	client := &http.Client{
		Transport: NewTransport(t.Context(), "", testServer.URL, "client-id", "", encodePKCS8PEM(t, key)),
	}

	resp, err := client.Get(testServer.URL + "/")
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorContains(t, err, "upstream token exchange failed")
	assert.ErrorContains(t, err, "boom")
}

func TestPrivateKeyFlowMalformedTokenResponse(t *testing.T) {
	t.Parallel()

	key := generateTestRSAKey(t)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("not-json"))
		require.NoError(t, err)
	}))
	defer testServer.Close()

	client := &http.Client{
		Transport: NewTransport(t.Context(), "", testServer.URL, "client-id", "", encodePKCS8PEM(t, key)),
	}

	resp, err := client.Get(testServer.URL + "/")
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to decode token response")
}
