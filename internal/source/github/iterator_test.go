// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

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
		handler       func(callCount *atomic.Int32) http.HandlerFunc
		expectedItems int
		expectedPages int
		expectErr     error
		errContains   string
	}{
		"single page no rel next": {
			handler: func(_ *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					// No Link header → single page
					json.NewEncoder(w).Encode([]map[string]any{{"id": 1}, {"id": 2}})
				}
			},
			expectedItems: 2,
			expectedPages: 1,
		},
		"multiple pages with rel next": {
			handler: func(callCount *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					page := int(callCount.Add(1))
					w.Header().Set("Content-Type", "application/json")
					if page < 3 {
						w.Header().Set("Link", `<http://example.com?page=`+string(rune('0'+page+1))+`>; rel="next"`)
					}
					json.NewEncoder(w).Encode([]map[string]any{{"id": page}})
				}
			},
			expectedItems: 3,
			expectedPages: 3,
		},
		"empty response returns ErrIteratorDone": {
			handler: func(_ *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode([]map[string]any{})
				}
			},
			expectedItems: 0,
			expectedPages: 0,
			expectErr:     ErrIteratorDone,
		},
		"server error on first page": {
			handler: func(_ *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"message":"internal error"}`))
				}
			},
			expectedItems: 0,
			expectedPages: 1,
			errContains:   "unexpected status 500",
		},
		"server error on later page": {
			handler: func(callCount *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					page := int(callCount.Add(1))
					w.Header().Set("Content-Type", "application/json")
					if page == 1 {
						w.Header().Set("Link", `<http://example.com?page=2>; rel="next"`)
						json.NewEncoder(w).Encode([]map[string]any{{"id": 1}})
						return
					}
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"message":"error"}`))
				}
			},
			expectedItems: 1,
			expectedPages: 2,
			errContains:   "unexpected status 500",
		},
		"rate limit exhausted stops iteration": {
			handler: func(_ *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("X-Ratelimit-Remaining", "0")
					w.Header().Set("X-Ratelimit-Reset", "1700000000")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"message":"rate limit"}`))
				}
			},
			expectedItems: 0,
			expectedPages: 1,
			errContains:   "rate limit exhausted",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var callCount atomic.Int32
			server := httptest.NewServer(tc.handler(&callCount))
			t.Cleanup(server.Close)

			c := &client{
				baseURL:    server.URL,
				org:        "test-org",
				token:      "test-token",
				pageSize:   100,
				httpClient: server.Client(),
			}

			it := c.newPageIterator("/any/path", "2026-03-10")

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

			// Account for the final call that returned the error
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
		json.NewEncoder(w).Encode([]map[string]any{{"id": 1}})
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:    server.URL,
		org:        "test-org",
		token:      "test-token",
		pageSize:   100,
		httpClient: server.Client(),
	}

	it := c.newPageIterator("/any/path", "2026-03-10")

	// First call returns data
	items, err := it.next(t.Context())
	require.NoError(t, err)
	assert.Len(t, items, 1)

	// Second call returns ErrIteratorDone (no Link header on first response)
	_, err = it.next(t.Context())
	require.ErrorIs(t, err, ErrIteratorDone)

	// Third call is still ErrIteratorDone (idempotent)
	_, err = it.next(t.Context())
	require.ErrorIs(t, err, ErrIteratorDone)
}

func TestPageIteratorContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", `<http://example.com?page=2>; rel="next"`)
		json.NewEncoder(w).Encode([]map[string]any{{"id": 1}})
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:    server.URL,
		org:        "test-org",
		token:      "test-token",
		pageSize:   100,
		httpClient: server.Client(),
	}

	it := c.newPageIterator("/any/path", "2026-03-10")

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

func TestHasRelNext(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		linkHeader string
		expected   bool
	}{
		"empty header": {
			linkHeader: "",
			expected:   false,
		},
		"has rel next": {
			linkHeader: `<https://api.github.com/orgs/my-org/repos?page=2&per_page=100>; rel="next", <https://api.github.com/orgs/my-org/repos?page=5&per_page=100>; rel="last"`,
			expected:   true,
		},
		"no rel next only last": {
			linkHeader: `<https://api.github.com/orgs/my-org/repos?page=5&per_page=100>; rel="last"`,
			expected:   false,
		},
		"rel next only": {
			linkHeader: `<https://api.github.com/orgs/my-org/repos?page=2>; rel="next"`,
			expected:   true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, hasRelNext(tc.linkHeader))
		})
	}
}

func TestWrappedPageIterator(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		handler       func(callCount *atomic.Int32) http.HandlerFunc
		responseKey   string
		expectedItems int
		expectedPages int
		expectErr     error
		errContains   string
	}{
		"wrapped response valid": {
			responseKey: "workflow_runs",
			handler: func(_ *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"total_count":1,"workflow_runs":[{"id":1}]}`))
				}
			},
			expectedItems: 1,
			expectedPages: 1,
		},
		"wrapped response with pagination": {
			responseKey: "workflow_runs",
			handler: func(callCount *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					page := int(callCount.Add(1))
					w.Header().Set("Content-Type", "application/json")
					if page < 2 {
						w.Header().Set("Link", `<http://example.com?page=2>; rel="next"`)
					}
					w.Write([]byte(`{"total_count":2,"workflow_runs":[{"id":` + string(rune('0'+page)) + `}]}`))
				}
			},
			expectedItems: 2,
			expectedPages: 2,
		},
		"wrapped response empty array": {
			responseKey: "workflow_runs",
			handler: func(_ *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"total_count":0,"workflow_runs":[]}`))
				}
			},
			expectedItems: 0,
			expectedPages: 0,
			expectErr:     ErrIteratorDone,
		},
		"wrapped response missing key": {
			responseKey: "workflow_runs",
			handler: func(_ *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"other_key":[]}`))
				}
			},
			expectedItems: 0,
			expectedPages: 1,
			errContains:   `response missing key "workflow_runs"`,
		},
		"wrapped response invalid inner array": {
			responseKey: "workflow_runs",
			handler: func(_ *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"workflow_runs":"not-an-array"}`))
				}
			},
			expectedItems: 0,
			expectedPages: 1,
			errContains:   `failed to decode "workflow_runs"`,
		},
		"rate limit exhausted stops iteration": {
			responseKey: "workflow_runs",
			handler: func(_ *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("X-Ratelimit-Remaining", "0")
					w.Header().Set("X-Ratelimit-Reset", "1700000000")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"message":"rate limit"}`))
				}
			},
			expectedItems: 0,
			expectedPages: 1,
			errContains:   "rate limit exhausted",
		},
		"non-2xx status returns error": {
			responseKey: "workflow_runs",
			handler: func(_ *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"message":"internal error"}`))
				}
			},
			expectedItems: 0,
			expectedPages: 1,
			errContains:   "unexpected status 500",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var callCount atomic.Int32
			server := httptest.NewServer(tc.handler(&callCount))
			t.Cleanup(server.Close)

			c := &client{
				baseURL:    server.URL,
				org:        "test-org",
				token:      "test-token",
				pageSize:   100,
				httpClient: server.Client(),
			}

			it := &wrappedPageIterator{
				client:      c,
				path:        "/any/path",
				apiVersion:  "2026-03-10",
				responseKey: tc.responseKey,
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

			// Account for the final call that returned the error
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

func TestWrappedPageIteratorIdempotentDone(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"workflow_runs":[{"id":1}]}`))
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:    server.URL,
		org:        "test-org",
		token:      "test-token",
		pageSize:   100,
		httpClient: server.Client(),
	}

	it := &wrappedPageIterator{
		client:      c,
		path:        "/any/path",
		apiVersion:  "2026-03-10",
		responseKey: "workflow_runs",
	}

	// First call returns data
	items, err := it.next(t.Context())
	require.NoError(t, err)
	assert.Len(t, items, 1)

	// Second call returns ErrIteratorDone (no Link header)
	_, err = it.next(t.Context())
	require.ErrorIs(t, err, ErrIteratorDone)

	// Third call is still ErrIteratorDone (idempotent)
	_, err = it.next(t.Context())
	require.ErrorIs(t, err, ErrIteratorDone)
}
