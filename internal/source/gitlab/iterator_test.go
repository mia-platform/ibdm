// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPageIterator(t *testing.T) {
	testCases := map[string]struct {
		handler       http.HandlerFunc
		expectedCount int
		expectErr     bool
	}{
		"single page": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-total-pages", "1")
				jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
			},
			expectedCount: 1,
		},
		"three pages all collected": {
			handler: func() http.HandlerFunc {
				var call atomic.Int32
				return func(w http.ResponseWriter, r *http.Request) {
					n := int(call.Add(1))
					w.Header().Set("x-total-pages", "3")
					jsonResponse(t, w, []map[string]any{{"page": float64(n)}})
				}
			}(),
			expectedCount: 3,
		},
		"empty list returns ErrIteratorDone immediately": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-total-pages", "0")
				jsonResponse(t, w, []map[string]any{})
			},
			expectedCount: 0,
		},
		"missing x-total-pages header": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				jsonResponse(t, w, []map[string]any{})
			},
			expectedCount: 0,
		},
		"server error on first page": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectErr: true,
		},
		"server error on second page": {
			handler: func() http.HandlerFunc {
				var call atomic.Int32
				return func(w http.ResponseWriter, r *http.Request) {
					if call.Add(1) == 1 {
						w.Header().Set("x-total-pages", "2")
						jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
						return
					}
					w.WriteHeader(http.StatusInternalServerError)
				}
			}(),
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestGitLabClient(t, srv)
			it := client.newPageIterator("/any/path")

			var all []map[string]any
			for {
				items, err := it.next(t.Context())
				if errors.Is(err, ErrIteratorDone) {
					break
				}
				if err != nil {
					if tc.expectErr {
						return
					}
					require.NoError(t, err)
				}
				all = append(all, items...)
			}

			if tc.expectErr {
				t.Fatal("expected error but iteration completed")
			}
			assert.Len(t, all, tc.expectedCount)
		})
	}

	t.Run("next after done returns ErrIteratorDone idempotently", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("x-total-pages", "1")
			jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
		}))
		defer srv.Close()

		client := newTestGitLabClient(t, srv)
		it := client.newPageIterator("/any/path")

		items, err := it.next(t.Context())
		require.NoError(t, err)
		require.Len(t, items, 1)

		_, err = it.next(t.Context())
		require.ErrorIs(t, err, ErrIteratorDone)

		_, err = it.next(t.Context())
		require.ErrorIs(t, err, ErrIteratorDone)
	})

	t.Run("pages arrive incrementally", func(t *testing.T) {
		var requestCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := int(requestCount.Add(1))
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			w.Header().Set("x-total-pages", "3")
			jsonResponse(t, w, []map[string]any{{"page": float64(page), "request": float64(n)}})
		}))
		defer srv.Close()

		client := newTestGitLabClient(t, srv)
		it := client.newPageIterator("/any/path")

		// First page
		items, err := it.next(t.Context())
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, 1, int(items[0]["page"].(float64)))

		// Second page
		items, err = it.next(t.Context())
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, 2, int(items[0]["page"].(float64)))

		// Third page
		items, err = it.next(t.Context())
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, 3, int(items[0]["page"].(float64)))

		// Done
		_, err = it.next(t.Context())
		require.ErrorIs(t, err, ErrIteratorDone)

		assert.EqualValues(t, 3, requestCount.Load())
	})
}
