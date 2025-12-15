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
		require.ErrorIs(t, err, env.VarIsNotSetError{Key: "MIA_CATALOG_ENDPOINT"})
		require.Nil(t, dest)
	})

	t.Run("with required env", func(t *testing.T) {
		t.Setenv("MIA_CATALOG_ENDPOINT", "http://localhost:8080/custom-catalog")
		dest, err := NewDestination()
		require.NoError(t, err)

		catalogDestination, ok := dest.(*catalogDestination)
		require.True(t, ok)

		assert.Equal(t, "http://localhost:8080/custom-catalog", catalogDestination.CatalogEndpoint)
		assert.Empty(t, catalogDestination.Token)
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
				Resource:   "resources",
				Name:       "test-data",
				Data: map[string]any{
					"key": "value",
				},
			},
			expectedBody: map[string]any{
				"apiVersion": "v1",
				"resource":   "resources",
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
				case "/invalid-error-response":
					http.NotFound(w, r)
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
				Resource:   "resources",
				Name:       "test-data",
			},
			expectedBody: map[string]any{
				"apiVersion": "v1",
				"resource":   "resources",
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

func TestInvalidURL(t *testing.T) {
	t.Parallel()

	dest := &catalogDestination{
		CatalogEndpoint: "http://%41:8080/", // invalid URL
	}

	err := dest.SendData(t.Context(), &destination.Data{})
	assert.ErrorIs(t, err, url.EscapeError("%41"))
}
