// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/destination"
	"github.com/mia-platform/ibdm/internal/info"
)

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

				if r.Method != http.MethodDelete {
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
