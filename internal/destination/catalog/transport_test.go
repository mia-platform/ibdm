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

	"github.com/lestrrat-go/jwx/v3/jwk"
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

// newTestKeys parses key into a jwk.Key and wraps it into the *Keys shape consumed by
// NewTransport, mirroring what LoadKeys would produce for a valid private key file.
func newTestKeys(t *testing.T, key *rsa.PrivateKey) *Keys {
	t.Helper()

	jwkKey, err := jwk.Import(key)
	require.NoError(t, err)

	return &Keys{PrivateKey: jwkKey}
}

func TestNewTransportWithoutCredentials(t *testing.T) {
	t.Parallel()

	transport := NewTransport(t.Context(), "", "", "", "", nil)
	assert.Same(t, http.DefaultTransport, transport)
}

// TestNewTransportPrivateKeyJWTWiring verifies that NewTransport wires the client-ID/private-key
// case to a jwtclientcredential provider by exercising a full token exchange plus authenticated
// request through the resulting transport. The JWT assertion contents and provider-level failure
// modes are covered by the jwtclientcredential package's own tests.
func TestNewTransportPrivateKeyJWTWiring(t *testing.T) {
	t.Parallel()

	key := generateTestRSAKey(t)
	const clientID = "jwt-bearer-client"

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, clientID, r.FormValue("client_id"))

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

	client := &http.Client{
		Transport: NewTransport(t.Context(), "", testServer.URL+"/oauth/token", clientID, "", newTestKeys(t, key)),
	}

	resp, err := client.Get(testServer.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}
