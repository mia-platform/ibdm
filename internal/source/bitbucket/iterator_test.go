// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPageIterator(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		handler       func(callCount *atomic.Int32, serverURL *string) http.HandlerFunc
		expectedItems int
		expectedPages int
		expectErr     error
		errContains   string
	}{
		"single page no next field": {
			handler: func(_ *atomic.Int32, _ *string) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{
						"values": []map[string]any{{"id": 1}, {"id": 2}},
					})
				}
			},
			expectedItems: 2,
			expectedPages: 1,
		},
		"multiple pages with next URL": {
			handler: func(callCount *atomic.Int32, serverURL *string) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					page := int(callCount.Add(1))
					w.Header().Set("Content-Type", "application/json")
					resp := map[string]any{
						"values": []map[string]any{{"id": page}},
					}
					if page < 3 {
						resp["next"] = *serverURL + "/2.0/test?page=" + string(rune('0'+page+1))
					}
					json.NewEncoder(w).Encode(resp)
				}
			},
			expectedItems: 3,
			expectedPages: 3,
		},
		"empty values array returns ErrIteratorDone": {
			handler: func(_ *atomic.Int32, _ *string) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{
						"values": []map[string]any{},
					})
				}
			},
			expectedItems: 0,
			expectedPages: 0,
			expectErr:     ErrIteratorDone,
		},
		"server error on first page": {
			handler: func(_ *atomic.Int32, _ *string) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"error":"internal error"}`))
				}
			},
			expectedItems: 0,
			expectedPages: 1,
			errContains:   "unexpected status 500",
		},
		"server error on later page": {
			handler: func(callCount *atomic.Int32, serverURL *string) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					page := int(callCount.Add(1))
					w.Header().Set("Content-Type", "application/json")
					if page == 1 {
						resp := map[string]any{
							"values": []map[string]any{{"id": 1}},
							"next":   *serverURL + "/2.0/test?page=2",
						}
						json.NewEncoder(w).Encode(resp)
						return
					}
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"error":"error"}`))
				}
			},
			expectedItems: 1,
			expectedPages: 2,
			errContains:   "unexpected status 500",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var callCount atomic.Int32
			var serverURL string
			server := httptest.NewServer(tc.handler(&callCount, &serverURL))
			serverURL = server.URL
			t.Cleanup(server.Close)

			c := &client{
				baseURL:    server.URL,
				httpClient: server.Client(),
			}

			it := &pageIterator{
				client:  c,
				nextURL: server.URL + "/2.0/test?pagelen=100",
			}

			var allItems []map[string]any
			pages := 0
			var lastErr error

			for {
				items, err := it.next(t.Context())
				if err != nil {
					lastErr = err
					break
				}
				pages++
				allItems = append(allItems, items...)
			}

			// Account for the final call that returned a non-done error
			if !errors.Is(lastErr, ErrIteratorDone) {
				pages++
			}

			assert.Len(t, allItems, tc.expectedItems)
			assert.Equal(t, tc.expectedPages, pages)

			if tc.expectErr != nil {
				require.ErrorIs(t, lastErr, tc.expectErr)
			} else if tc.errContains != "" {
				require.Error(t, lastErr)
				assert.Contains(t, lastErr.Error(), tc.errContains)
			}
		})
	}
}

func TestPageIteratorIdempotentDone(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{{"id": 1}},
		})
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	it := &pageIterator{
		client:  c,
		nextURL: server.URL + "/2.0/test",
	}

	// First call returns data
	items, err := it.next(t.Context())
	require.NoError(t, err)
	assert.Len(t, items, 1)

	// Second call returns ErrIteratorDone (no next URL in response)
	_, err = it.next(t.Context())
	require.ErrorIs(t, err, ErrIteratorDone)

	// Third call is still ErrIteratorDone (idempotent)
	_, err = it.next(t.Context())
	require.ErrorIs(t, err, ErrIteratorDone)
}

func TestPageIteratorContextCancellation(t *testing.T) {
	t.Parallel()

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{{"id": 1}},
			"next":   serverURL + "/2.0/test?page=2",
		})
	}))
	serverURL = server.URL
	t.Cleanup(server.Close)

	c := &client{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	it := &pageIterator{
		client:  c,
		nextURL: server.URL + "/2.0/test",
	}

	ctx, cancel := context.WithCancel(t.Context())

	// First call succeeds
	items, err := it.next(ctx)
	require.NoError(t, err)
	assert.Len(t, items, 1)

	// Cancel context before second call
	cancel()

	_, err = it.next(ctx)
	require.Error(t, err)
}

func TestPageIteratorNextURLUsedAsIs(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	var receivedURLs []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURLs = append(receivedURLs, r.URL.String())
		page := int(callCount.Add(1))
		w.Header().Set("Content-Type", "application/json")
		if page == 1 {
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{{"id": 1}},
				"next":   "http://" + r.Host + "/2.0/test?page=2&cursor=abc123",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{{"id": 2}},
		})
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	it := &pageIterator{
		client:  c,
		nextURL: server.URL + "/2.0/test?pagelen=100",
	}

	// First page
	items, err := it.next(t.Context())
	require.NoError(t, err)
	assert.Len(t, items, 1)

	// Second page — should use the next URL as-is
	items, err = it.next(t.Context())
	require.NoError(t, err)
	assert.Len(t, items, 1)

	// Verify the second request used the exact next URL from the response
	require.Len(t, receivedURLs, 2)
	assert.Equal(t, "/2.0/test?pagelen=100", receivedURLs[0])
	assert.Equal(t, "/2.0/test?page=2&cursor=abc123", receivedURLs[1])
}

func TestPageIteratorNetworkError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Respond with single page first, so next URL is set
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{{"id": 1}},
			"next":   "http://127.0.0.1:0/unreachable",
		})
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	it := &pageIterator{
		client:  c,
		nextURL: server.URL + "/2.0/test",
	}

	// First call succeeds
	items, err := it.next(t.Context())
	require.NoError(t, err)
	assert.Len(t, items, 1)

	// Second call hits unreachable URL → network error → iterator marks done
	_, err = it.next(t.Context())
	require.Error(t, err)

	// Third call is idempotent done
	_, err = it.next(t.Context())
	require.ErrorIs(t, err, ErrIteratorDone)
}
