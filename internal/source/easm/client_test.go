// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package easm

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		config       config
		expectErr    error
		assertClient func(t *testing.T, c *client)
	}{
		"valid base URL": {
			config: config{
				BaseURL:     "https://easm.example.com",
				DataPath:    "/data",
				Customer:    "acme",
				Token:       "test-token",
				HTTPTimeout: 5 * time.Second,
			},
			assertClient: func(t *testing.T, c *client) {
				t.Helper()
				assert.Equal(t, "https://easm.example.com", c.baseURL.String())
				assert.Equal(t, "/data", c.dataPath)
				assert.Equal(t, "acme", c.customer)
				assert.Equal(t, "test-token", c.token)
				assert.Equal(t, 5*time.Second, c.httpClient.Timeout)
			},
		},
		"invalid base URL": {
			config: config{
				BaseURL: "://invalid",
			},
			expectErr: ErrInvalidEnvVariable,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c, err := newClient(tc.config)
			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
				return
			}

			require.NoError(t, err)
			tc.assertClient(t, c)
		})
	}
}

func TestFetchDataPagePagination(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		body           string
		nextCursor     string
		expectedItems  int
		expectedCursor string
	}{
		"first page with next cursor": {
			body:           `[{"id":"1","type":"domain"},{"id":"2","type":"host"}]`,
			nextCursor:     "cursor-2",
			expectedItems:  2,
			expectedCursor: "cursor-2",
		},
		"last page without cursor": {
			body:           `[{"id":"3","type":"ip"}]`,
			nextCursor:     "",
			expectedItems:  1,
			expectedCursor: "",
		},
		"empty page": {
			body:           `[]`,
			nextCursor:     "",
			expectedItems:  0,
			expectedCursor: "",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.nextCursor != "" {
					w.Header().Set(nextCursorHeader, tc.nextCursor)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			})

			source := newTestSource(t, handler)

			page, err := source.client.fetchDataPage(t.Context(), "")
			require.NoError(t, err)
			assert.Len(t, page.items, tc.expectedItems)
			assert.Equal(t, tc.expectedCursor, page.nextCursor)
		})
	}
}

func TestFetchDataPageSendsCursor(t *testing.T) {
	t.Parallel()

	var gotCursor string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCursor = r.URL.Query().Get(cursorQueryParam)
		_, _ = w.Write([]byte(`[]`))
	})

	source := newTestSource(t, handler)

	_, err := source.client.fetchDataPage(t.Context(), "cursor-42")
	require.NoError(t, err)
	assert.Equal(t, "cursor-42", gotCursor)
}

func TestFetchDataPageHeaders(t *testing.T) {
	t.Parallel()

	t.Run("token and customer set", func(t *testing.T) {
		t.Parallel()

		var gotReq *http.Request
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotReq = r.Clone(r.Context())
			_, _ = w.Write([]byte(`[]`))
		})
		source := newTestSource(t, handler)

		_, err := source.client.fetchDataPage(t.Context(), "")
		require.NoError(t, err)
		assert.Equal(t, "application/json", gotReq.Header.Get("Accept"))
		assert.Equal(t, "Bearer test-token", gotReq.Header.Get("Authorization"))
		assert.Equal(t, "acme", gotReq.Header.Get("X-Customer"))
	})

	t.Run("empty token omits Authorization header", func(t *testing.T) {
		t.Parallel()

		var gotReq *http.Request
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotReq = r.Clone(r.Context())
			_, _ = w.Write([]byte(`[]`))
		})
		source := newTestSource(t, handler)
		// Build a client without a token pointing at the same server.
		c := &client{
			baseURL:    source.client.baseURL,
			dataPath:   source.client.dataPath,
			customer:   source.client.customer,
			token:      "",
			httpClient: source.client.httpClient,
		}

		_, err := c.fetchDataPage(t.Context(), "")
		require.NoError(t, err)
		assert.Empty(t, gotReq.Header.Get("Authorization"))
		assert.Equal(t, "acme", gotReq.Header.Get("X-Customer"))
	})
}

func TestFetchDataPageErrors(t *testing.T) {
	t.Parallel()

	t.Run("non-200 status", func(t *testing.T) {
		t.Parallel()

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		})

		source := newTestSource(t, handler)

		_, err := source.client.fetchDataPage(t.Context(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "500")
		assert.Contains(t, err.Error(), "boom")
	})

	t.Run("malformed JSON body", func(t *testing.T) {
		t.Parallel()

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not json"))
		})

		source := newTestSource(t, handler)

		_, err := source.client.fetchDataPage(t.Context(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode data response")
	})
}
