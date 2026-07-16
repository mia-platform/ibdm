// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	lestrratjwk "github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/caarlos0/env/v11"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/destination"
	"github.com/mia-platform/ibdm/internal/info"
	"github.com/mia-platform/ibdm/internal/jwk"
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

func TestInitialization(t *testing.T) {
	t.Run("without envs", func(t *testing.T) {
		dest, err := NewDestination()
		assert.ErrorIs(t, err, env.VarIsNotSetError{Key: "MIA_CATALOG_ENDPOINT"})
		assert.Nil(t, dest)
	})

	t.Run("with required env", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		dest, err := NewDestination()
		require.NoError(t, err)
		catalogDestination, ok := dest.(*catalogDestination)
		require.True(t, ok)

		assert.Equal(t, "http://localhost:8080/custom-catalog", catalogDestination.CatalogEndpoint)
		assert.Empty(t, catalogDestination.Token)
		assert.Empty(t, catalogDestination.ClientID)
		assert.Empty(t, catalogDestination.ClientSecret)
		assert.Equal(t, "http://localhost:8080/oauth/token", catalogDestination.AuthEndpoint)
	})

	t.Run("with all envs", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_TOKEN", "test-token2")
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		dest, err := NewDestination()
		require.NoError(t, err)
		catalogDestination, ok := dest.(*catalogDestination)
		require.True(t, ok)

		assert.Equal(t, "http://localhost:8080/custom-catalog", catalogDestination.CatalogEndpoint)
		assert.Equal(t, "test-token2", catalogDestination.Token)
		assert.Empty(t, catalogDestination.ClientID)
		assert.Empty(t, catalogDestination.ClientSecret)
		assert.Equal(t, "http://localhost:8080/oauth/token", catalogDestination.AuthEndpoint)
	})

	t.Run("without explicit auth endpoint", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_CLIENT_ID", "client-id")
		t.Setenv("MIA_CATALOG_CLIENT_SECRET", "client-secret")
		dest, err := NewDestination()
		assert.NoError(t, err)
		catalogDestination, ok := dest.(*catalogDestination)
		require.True(t, ok)

		assert.Equal(t, "http://localhost:8080/custom-catalog", catalogDestination.CatalogEndpoint)
		assert.Empty(t, catalogDestination.Token)
		assert.Equal(t, "client-id", catalogDestination.ClientID)
		assert.Equal(t, "client-secret", catalogDestination.ClientSecret)
		assert.Equal(t, "http://localhost:8080/oauth/token", catalogDestination.AuthEndpoint)
	})

	t.Run("with explicit auth endpoint", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_CLIENT_ID", "client-id")
		t.Setenv("MIA_CATALOG_CLIENT_SECRET", "client-secret")
		t.Setenv("MIA_CATALOG_AUTH_ENDPOINT", "http://localhost:8081/custom/auth")
		dest, err := NewDestination()
		assert.NoError(t, err)
		catalogDestination, ok := dest.(*catalogDestination)
		require.True(t, ok)

		assert.Equal(t, "http://localhost:8080/custom-catalog", catalogDestination.CatalogEndpoint)
		assert.Empty(t, catalogDestination.Token)
		assert.Equal(t, "client-id", catalogDestination.ClientID)
		assert.Equal(t, "client-secret", catalogDestination.ClientSecret)
		assert.Equal(t, "http://localhost:8081/custom/auth", catalogDestination.AuthEndpoint)
	})

	t.Run("with invalid endpoint URL", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://%41:8080/") // invalid URL
		dest, err := NewDestination()
		assert.ErrorIs(t, err, url.EscapeError("%41"))
		assert.Nil(t, dest)
	})

	t.Run("with both env for fixed token and client credentials", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_TOKEN", "test-token")
		t.Setenv("MIA_CATALOG_CLIENT_ID", "client-id")
		dest, err := NewDestination()
		assert.ErrorIs(t, err, errMultipleAuthMethods)
		assert.Nil(t, dest)
	})

	t.Run("missing secret with client id", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_CLIENT_ID", "client-id")
		dest, err := NewDestination()
		assert.ErrorIs(t, err, errMissingClientSecret)
		assert.Nil(t, dest)
	})

	t.Run("missing secret with client id", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_CLIENT_SECRET", "client-secret")
		dest, err := NewDestination()
		assert.ErrorIs(t, err, errMissingClientID)
		assert.Nil(t, dest)
	})

	t.Run("invalid auth endpoint", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_AUTH_ENDPOINT", "http://%41:8080/") // invalid URL
		dest, err := NewDestination()
		assert.ErrorIs(t, err, url.EscapeError("%41"))
		assert.Nil(t, dest)
	})

	t.Run("invalid issuer", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_ISSUER", "http://%41:8080/") // invalid URL
		dest, err := NewDestination()
		assert.ErrorIs(t, err, url.EscapeError("%41"))
		assert.Nil(t, dest)
	})

	t.Run("invalid issuer metadata", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_ISSUER_METADATA", "http://%41:8080/") // invalid URL
		dest, err := NewDestination()
		assert.ErrorIs(t, err, url.EscapeError("%41"))
		assert.Nil(t, dest)
	})

	t.Run("invalid token endpoint", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_TOKEN_ENDPOINT", "http://%41:8080/") // invalid URL
		dest, err := NewDestination()
		assert.ErrorIs(t, err, url.EscapeError("%41"))
		assert.Nil(t, dest)
	})

	t.Run("with private key and client id", func(t *testing.T) {
		keyPath := filepath.Join(t.TempDir(), "private-key.pem")
		writeTestFile(t, keyPath, encodePKCS8PEM(t, generateTestRSAKey(t)))

		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_CLIENT_ID", "client-id")
		t.Setenv("MIA_CATALOG_PRIVATE_KEY_PATH", keyPath)
		t.Setenv("MIA_CATALOG_ISSUER", "http://localhost:8081/issuer")
		dest, err := NewDestination()
		require.NoError(t, err)
		catalogDestination, ok := dest.(*catalogDestination)
		require.True(t, ok)

		assert.Equal(t, "client-id", catalogDestination.ClientID)
		assert.Equal(t, keyPath, catalogDestination.PrivateKeyPath)
		assert.Empty(t, catalogDestination.ClientSecret)
		assert.Equal(t, "http://localhost:8081/issuer", catalogDestination.Issuer)
		// AuthEndpoint keeps its client-credentials default even in private-key mode, where it is
		// unused: it is not consulted by the private-key JWT branch.
		assert.Equal(t, "http://localhost:8080/oauth/token", catalogDestination.AuthEndpoint)
		require.NotNil(t, catalogDestination.keys)
		assert.NotNil(t, catalogDestination.keys.PrivateKey)
	})

	t.Run("private key without issuer config", func(t *testing.T) {
		keyPath := filepath.Join(t.TempDir(), "private-key.pem")
		writeTestFile(t, keyPath, encodePKCS8PEM(t, generateTestRSAKey(t)))

		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_CLIENT_ID", "client-id")
		t.Setenv("MIA_CATALOG_PRIVATE_KEY_PATH", keyPath)
		dest, err := NewDestination()
		assert.ErrorIs(t, err, errMissingIssuerConfig)
		assert.Nil(t, dest)
	})

	t.Run("with custom scope", func(t *testing.T) {
		keyPath := filepath.Join(t.TempDir(), "private-key.pem")
		writeTestFile(t, keyPath, encodePKCS8PEM(t, generateTestRSAKey(t)))

		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_CLIENT_ID", "client-id")
		t.Setenv("MIA_CATALOG_PRIVATE_KEY_PATH", keyPath)
		t.Setenv("MIA_CATALOG_ISSUER", "http://localhost:8081/issuer")
		t.Setenv("MIA_CATALOG_CUSTOM_SCOPE", "organization:custom")
		dest, err := NewDestination()
		require.NoError(t, err)
		catalogDestination, ok := dest.(*catalogDestination)
		require.True(t, ok)

		assert.Equal(t, "organization:custom", catalogDestination.CustomScope)
	})

	t.Run("without custom scope", func(t *testing.T) {
		keyPath := filepath.Join(t.TempDir(), "private-key.pem")
		writeTestFile(t, keyPath, encodePKCS8PEM(t, generateTestRSAKey(t)))

		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_CLIENT_ID", "client-id")
		t.Setenv("MIA_CATALOG_PRIVATE_KEY_PATH", keyPath)
		t.Setenv("MIA_CATALOG_ISSUER", "http://localhost:8081/issuer")
		dest, err := NewDestination()
		require.NoError(t, err)
		catalogDestination, ok := dest.(*catalogDestination)
		require.True(t, ok)

		// MIA_CATALOG_CUSTOM_SCOPE has no default: when unset the scope must remain empty.
		assert.Empty(t, catalogDestination.CustomScope)
	})

	t.Run("with private key, client id and custom issuer metadata/token endpoint", func(t *testing.T) {
		keyPath := filepath.Join(t.TempDir(), "private-key.pem")
		writeTestFile(t, keyPath, encodePKCS8PEM(t, generateTestRSAKey(t)))

		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_CLIENT_ID", "client-id")
		t.Setenv("MIA_CATALOG_PRIVATE_KEY_PATH", keyPath)
		t.Setenv("MIA_CATALOG_ISSUER_METADATA", "http://localhost:8081/custom/metadata")
		t.Setenv("MIA_CATALOG_TOKEN_ENDPOINT", "http://localhost:8081/custom/token")
		dest, err := NewDestination()
		require.NoError(t, err)
		catalogDestination, ok := dest.(*catalogDestination)
		require.True(t, ok)

		assert.Equal(t, "http://localhost:8081/custom/metadata", catalogDestination.IssuerMetadata)
		assert.Equal(t, "http://localhost:8081/custom/token", catalogDestination.TokenEndpoint)
	})

	t.Run("private key with unreadable file", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_CLIENT_ID", "client-id")
		t.Setenv("MIA_CATALOG_PRIVATE_KEY_PATH", filepath.Join(t.TempDir(), "does-not-exist.pem"))
		t.Setenv("MIA_CATALOG_ISSUER", "http://localhost:8081/issuer")
		dest, err := NewDestination()
		assert.ErrorContains(t, err, "cannot read private key from")
		assert.Nil(t, dest)
	})

	t.Run("private key without client id", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_PRIVATE_KEY_PATH", "fictional-private-key-path")
		t.Setenv("MIA_CATALOG_ISSUER", "http://localhost:8081/issuer")
		dest, err := NewDestination()
		assert.ErrorIs(t, err, errMissingClientIDForPrivKey)
		assert.Nil(t, dest)
	})

	t.Run("private key with client secret", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_CLIENT_ID", "client-id")
		t.Setenv("MIA_CATALOG_CLIENT_SECRET", "client-secret")
		t.Setenv("MIA_CATALOG_PRIVATE_KEY_PATH", "fictional-private-key-path")
		dest, err := NewDestination()
		assert.ErrorIs(t, err, errPrivateKeyWithClientSecret)
		assert.Nil(t, dest)
	})

	t.Run("private key with fixed token", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		t.Setenv("MIA_CATALOG_TOKEN", "test-token")
		t.Setenv("MIA_CATALOG_PRIVATE_KEY_PATH", "fictional-private-key-path")
		dest, err := NewDestination()
		assert.ErrorIs(t, err, errMultipleAuthMethods)
		assert.Nil(t, dest)
	})
}

func TestSendData(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		endpoint      string
		data          *destination.Data
		expectedBody  map[string]any
		expectedError error
	}{
		"successful send": {
			endpoint: "/valid-endpoint",
			data: &destination.Data{
				APIVersion: "v1",
				ItemFamily: "family",
				Name:       "test-data",
				Data: map[string]any{
					"key": "value",
				},
			},
			expectedBody: map[string]any{
				"apiVersion": "v1",
				"itemFamily": "family",
				"name":       "test-data",
				"operation":  "upsert",
				"data": map[string]any{
					"key": "value",
				},
			},
		},
		"failed send": {
			endpoint: "/invalid-endpoint",
			data: &destination.Data{
				APIVersion: "v1",
			},
			expectedError: &CatalogError{err: errors.New("error message")},
		},
		"unauthorized send": {
			endpoint: "/unauthorized-endpoint",
			data: &destination.Data{
				APIVersion: "v1",
			},
			expectedError: &CatalogError{err: errors.New("invalid token or insufficient permissions")},
		},
		"not found send": {
			endpoint: "/not-found-endpoint",
			data: &destination.Data{
				APIVersion: "v1",
			},
			expectedError: &CatalogError{err: errors.New("integration registration not found")},
		},
		"invalid error response": {
			endpoint: "/invalid-error-response",
			data: &destination.Data{
				APIVersion: "v1",
			},
			expectedError: &CatalogError{err: errors.New("unexpected error")},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Body != nil {
					defer r.Body.Close()
				}

				if r.Method != http.MethodPost {
					http.Error(w, "invalid method", http.StatusMethodNotAllowed)
					return
				}

				// check headers
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				assert.Equal(t, info.AppName+"/"+info.Version, r.Header.Get("User-Agent"))

				switch r.RequestURI {
				case "/valid-endpoint":
					decodedBody := make(map[string]any)
					decoder := json.NewDecoder(r.Body)
					err := decoder.Decode(&decodedBody)
					assert.NoError(t, err)
					assert.Equal(t, tc.expectedBody, decodedBody)
					w.WriteHeader(http.StatusNoContent)
					return
				case "/not-found-endpoint":
					http.NotFound(w, r)
				case "/invalid-error-response":
					http.Error(w, "unexpected error", http.StatusBadGateway)
				case "/unauthorized-endpoint":
					http.Error(w, "unauthorized", http.StatusUnauthorized)
				default:
					errCode := http.StatusInternalServerError
					w.WriteHeader(errCode)

					encoder := json.NewEncoder(w)
					err := encoder.Encode(map[string]any{
						"statusCode": errCode,
						"error":      http.StatusText(errCode),
						"message":    "error message",
					})
					assert.NoError(t, err)
					return
				}
			}))
			defer testServer.Close()

			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			dest := &catalogDestination{
				CatalogEndpoint: testServer.URL + tc.endpoint,
				Token:           "test-token",
			}

			err := dest.SendData(ctx, tc.data)
			if tc.expectedError != nil {
				assert.ErrorIs(t, err, tc.expectedError)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestDeleteData(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		endpoint      string
		data          *destination.Data
		expectedBody  map[string]any
		expectedError error
	}{
		"successful delete": {
			endpoint: "/valid-endpoint",
			data: &destination.Data{
				APIVersion: "v1",
				ItemFamily: "family",
				Name:       "test-data",
			},
			expectedBody: map[string]any{
				"apiVersion": "v1",
				"itemFamily": "family",
				"name":       "test-data",
				"operation":  "delete",
			},
		},
		"failed delete": {
			endpoint: "/invalid-endpoint",
			data: &destination.Data{
				APIVersion: "v1",
			},
			expectedError: &CatalogError{err: errors.New("error message")},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Body != nil {
					defer r.Body.Close()
				}

				if r.Method != http.MethodPost {
					http.Error(w, "invalid method", http.StatusMethodNotAllowed)
					return
				}

				// check headers
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				assert.Equal(t, info.AppName+"/"+info.Version, r.Header.Get("User-Agent"))

				switch r.RequestURI {
				case "/valid-endpoint":
					decodedBody := make(map[string]any)
					decoder := json.NewDecoder(r.Body)
					err := decoder.Decode(&decodedBody)
					assert.NoError(t, err)
					assert.Equal(t, tc.expectedBody, decodedBody)
					w.WriteHeader(http.StatusNoContent)
					return
				default:
					errCode := http.StatusInternalServerError
					w.WriteHeader(errCode)

					encoder := json.NewEncoder(w)
					err := encoder.Encode(map[string]any{
						"statusCode": errCode,
						"error":      http.StatusText(errCode),
						"message":    "error message",
					})
					assert.NoError(t, err)
					return
				}
			}))
			defer testServer.Close()

			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			dest := &catalogDestination{
				CatalogEndpoint: testServer.URL + tc.endpoint,
				Token:           "test-token",
			}

			err := dest.DeleteData(ctx, tc.data)
			if tc.expectedError != nil {
				assert.ErrorIs(t, err, tc.expectedError)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestContextCancelled(t *testing.T) {
	t.Parallel()

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer r.Body.Close()
		}
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	dest := &catalogDestination{
		CatalogEndpoint: testServer.URL,
	}

	err := dest.SendData(ctx, &destination.Data{})
	assert.NoError(t, err)
}

func TestClientCredentialFlow(t *testing.T) {
	t.Parallel()

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer r.Body.Close()
		}
		if r.Method == http.MethodPost && r.RequestURI == "/oauth/token" {
			err := r.ParseForm()
			assert.NoError(t, err)
			assert.Equal(t, "client_credentials", r.FormValue("grant_type"))
			assert.Equal(t, "Basic dGVzdC1jbGllbnQtaWQ6dGVzdC1jbGllbnQtc2VjcmV0", r.Header.Get("Authorization"))

			w.Header().Set("Content-Type", "application/json")
			encoder := json.NewEncoder(w)
			err = encoder.Encode(map[string]any{
				"access_token": "generated-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			assert.NoError(t, err)
			return
		}

		if r.Method == http.MethodPost && r.RequestURI == "/" {
			assert.Equal(t, "Bearer generated-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer testServer.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	dest := &catalogDestination{
		CatalogEndpoint: testServer.URL + "/",
		ClientID:        "test-client-id",
		ClientSecret:    "test-client-secret",
		AuthEndpoint:    testServer.URL + "/oauth/token",
	}

	err := dest.SendData(ctx, &destination.Data{})
	assert.NoError(t, err)
}

// newTestPrivateKeyFor wraps key into a jwk.Keys usable as catalogDestination.keys, to be used as
// fictional test material. It is never used outside of this test file.
func newTestPrivateKeyFor(t *testing.T, key *rsa.PrivateKey) *jwk.Keys {
	t.Helper()

	jwkKey, err := lestrratjwk.Import(key)
	require.NoError(t, err)

	return &jwk.Keys{PrivateKey: jwkKey}
}

// TestPrivateKeyJWTFlowWithExplicitTokenEndpoint verifies that a catalogDestination configured
// with MIA_CATALOG_TOKEN_ENDPOINT reaches oauth2source.NewSource with that value, skipping OIDC
// discovery entirely: the discovery endpoint is registered but never hit, and the token exchange
// goes straight to the configured token endpoint.
func TestPrivateKeyJWTFlowWithExplicitTokenEndpoint(t *testing.T) {
	t.Parallel()

	key := generateTestRSAKey(t)
	var discoveryHits atomic.Int32

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer r.Body.Close()
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/.well-known/openid-configuration":
			discoveryHits.Add(1)
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/custom/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "test-client-id", r.FormValue("client_id"))
			assert.Equal(t, "private_key_jwt", r.FormValue("token_endpoint_auth_method"))

			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]any{
				"access_token": "generated-jwt-bearer-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			require.NoError(t, err)
		case r.Method == http.MethodPost && r.URL.Path == "/":
			assert.Equal(t, "Bearer generated-jwt-bearer-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	dest := &catalogDestination{
		CatalogEndpoint: testServer.URL + "/",
		ClientID:        "test-client-id",
		Issuer:          testServer.URL,
		TokenEndpoint:   testServer.URL + "/custom/token",
		keys:            newTestPrivateKeyFor(t, key),
	}

	err := dest.SendData(ctx, &destination.Data{})
	require.NoError(t, err)
	assert.Equal(t, int32(0), discoveryHits.Load())
}

// TestPrivateKeyJWTFlowWithCustomScope verifies that a catalogDestination configured with a custom
// scope forwards it through NewTransport to the token request as the "scope" form field, and that
// when no custom scope is configured the token request carries no scope (the default was removed).
func TestPrivateKeyJWTFlowWithCustomScope(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		customScope   string
		expectedScope string
	}{
		"with custom scope": {
			customScope:   "organization:custom",
			expectedScope: "organization:custom",
		},
		"without custom scope": {
			customScope:   "",
			expectedScope: "",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			key := generateTestRSAKey(t)

			testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Body != nil {
					defer r.Body.Close()
				}

				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/custom/token":
					require.NoError(t, r.ParseForm())
					assert.Equal(t, "test-client-id", r.FormValue("client_id"))
					assert.Equal(t, tc.expectedScope, r.FormValue("scope"))

					w.Header().Set("Content-Type", "application/json")
					err := json.NewEncoder(w).Encode(map[string]any{
						"access_token": "generated-jwt-bearer-token",
						"token_type":   "Bearer",
						"expires_in":   3600,
					})
					require.NoError(t, err)
				case r.Method == http.MethodPost && r.URL.Path == "/":
					assert.Equal(t, "Bearer generated-jwt-bearer-token", r.Header.Get("Authorization"))
					w.WriteHeader(http.StatusNoContent)
				default:
					http.NotFound(w, r)
				}
			}))
			defer testServer.Close()

			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			dest := &catalogDestination{
				CatalogEndpoint: testServer.URL + "/",
				ClientID:        "test-client-id",
				Issuer:          testServer.URL,
				TokenEndpoint:   testServer.URL + "/custom/token",
				CustomScope:     tc.customScope,
				keys:            newTestPrivateKeyFor(t, key),
			}

			err := dest.SendData(ctx, &destination.Data{})
			require.NoError(t, err)
		})
	}
}

// TestPrivateKeyJWTFlowWithCustomDiscoveryMetadata verifies that a catalogDestination configured
// with MIA_CATALOG_ISSUER_METADATA reaches oauth2source.NewSource with that value, so discovery is
// fetched from the custom URL rather than the default well-known path.
func TestPrivateKeyJWTFlowWithCustomDiscoveryMetadata(t *testing.T) {
	t.Parallel()

	key := generateTestRSAKey(t)
	var defaultDiscoveryHits atomic.Int32

	var testServer *httptest.Server
	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer r.Body.Close()
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/.well-known/openid-configuration":
			defaultDiscoveryHits.Add(1)
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/custom/metadata":
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]string{
				"issuer":         testServer.URL,
				"token_endpoint": testServer.URL + "/oauth/token",
			})
			require.NoError(t, err)
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "test-client-id", r.FormValue("client_id"))

			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]any{
				"access_token": "generated-jwt-bearer-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			require.NoError(t, err)
		case r.Method == http.MethodPost && r.URL.Path == "/":
			assert.Equal(t, "Bearer generated-jwt-bearer-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	dest := &catalogDestination{
		CatalogEndpoint: testServer.URL + "/",
		ClientID:        "test-client-id",
		Issuer:          testServer.URL,
		IssuerMetadata:  testServer.URL + "/custom/metadata",
		keys:            newTestPrivateKeyFor(t, key),
	}

	err := dest.SendData(ctx, &destination.Data{})
	require.NoError(t, err)
	assert.Equal(t, int32(0), defaultDiscoveryHits.Load())
}

// TestPrivateKeyJWTFlowWithMetadataOnlyNoIssuer verifies that a catalogDestination configured with
// MIA_CATALOG_ISSUER_METADATA but without MIA_CATALOG_ISSUER still completes the private-key JWT
// flow: with no configured issuer there is no expected issuer to validate the discovery document
// against, so the discovery document's issuer (which here has no relationship to the test server
// at all) must not cause a failure.
func TestPrivateKeyJWTFlowWithMetadataOnlyNoIssuer(t *testing.T) {
	t.Parallel()

	key := generateTestRSAKey(t)

	var testServer *httptest.Server
	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer r.Body.Close()
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/custom/metadata":
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]string{
				"issuer":         "https://unrelated-issuer.example.com",
				"token_endpoint": testServer.URL + "/oauth/token",
			})
			require.NoError(t, err)
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "test-client-id", r.FormValue("client_id"))

			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]any{
				"access_token": "generated-jwt-bearer-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			require.NoError(t, err)
		case r.Method == http.MethodPost && r.URL.Path == "/":
			assert.Equal(t, "Bearer generated-jwt-bearer-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	dest := &catalogDestination{
		CatalogEndpoint: testServer.URL + "/",
		ClientID:        "test-client-id",
		IssuerMetadata:  testServer.URL + "/custom/metadata",
		keys:            newTestPrivateKeyFor(t, key),
	}

	err := dest.SendData(ctx, &destination.Data{})
	require.NoError(t, err)
}

// TestPrivateKeyJWTFlowThroughNewDestination exercises the whole private-key JWT stack built by
// NewDestination from environment variables (rather than a hand-assembled struct), for the case
// where only MIA_CATALOG_ISSUER is configured: OIDC discovery is performed against the issuer's
// well-known path, the resolved token endpoint mints a bearer token, and the publish request
// carries it. This guards the construction path that wires MIA_CATALOG_ISSUER into oauth2source.
func TestPrivateKeyJWTFlowThroughNewDestination(t *testing.T) {
	// Not parallel: mutates the process environment via t.Setenv.
	var testServer *httptest.Server
	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer r.Body.Close()
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]string{
				"issuer":         testServer.URL,
				"token_endpoint": testServer.URL + "/oauth/token",
			})
			require.NoError(t, err)
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "test-client-id", r.FormValue("client_id"))
			assert.Equal(t, "private_key_jwt", r.FormValue("token_endpoint_auth_method"))

			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]any{
				"access_token": "generated-jwt-bearer-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			require.NoError(t, err)
		case r.Method == http.MethodPost && r.URL.Path == "/":
			assert.Equal(t, "Bearer generated-jwt-bearer-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	keyPath := filepath.Join(t.TempDir(), "private-key.pem")
	writeTestFile(t, keyPath, encodePKCS8PEM(t, generateTestRSAKey(t)))

	t.Setenv("MIA_CATALOG_ENDPOINT", testServer.URL+"/")
	t.Setenv("MIA_CATALOG_CLIENT_ID", "test-client-id")
	t.Setenv("MIA_CATALOG_PRIVATE_KEY_PATH", keyPath)
	t.Setenv("MIA_CATALOG_ISSUER", testServer.URL)

	dest, err := NewDestination()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	err = dest.SendData(ctx, &destination.Data{})
	require.NoError(t, err)
}
