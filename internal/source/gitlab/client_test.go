// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeRequest(t *testing.T) {
	testCases := map[string]struct {
		handler       http.HandlerFunc
		expectedItems []map[string]any
		expectedCurr  int
		expectedTotal int
		expectErr     bool
	}{
		"successful single page": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "test-token", r.Header.Get("PRIVATE-TOKEN"))
				assert.Equal(t, "application/json", r.Header.Get("Accept"))
				assert.Equal(t, "1", r.URL.Query().Get("page"))
				w.Header().Set("x-page", "1")
				w.Header().Set("x-total-pages", "1")
				jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
			},
			expectedItems: []map[string]any{{"id": float64(1)}},
			expectedCurr:  1,
			expectedTotal: 1,
		},
		"pagination headers parsed": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-page", "2")
				w.Header().Set("x-total-pages", "5")
				jsonResponse(t, w, []map[string]any{{"id": float64(2)}})
			},
			expectedItems: []map[string]any{{"id": float64(2)}},
			expectedCurr:  2,
			expectedTotal: 5,
		},
		"non-200 status": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectErr: true,
		},
		"malformed JSON": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-page", "1")
				w.Header().Set("x-total-pages", "1")
				_, _ = w.Write([]byte("not-json"))
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestGitLabClient(t, srv)
			items, currPage, totalPages, err := client.makePageableRequest(t.Context(), "/api/v4/projects", "per_page=100", 1)

			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedItems, items)
			assert.Equal(t, tc.expectedCurr, currPage)
			assert.Equal(t, tc.expectedTotal, totalPages)
		})
	}
}

func TestListAllPages(t *testing.T) {
	maxPagesLimit = 3
	testCases := map[string]struct {
		pages         int // total pages the server reports
		expectedCount int
		expectErr     bool
	}{
		"single page": {
			pages:         1,
			expectedCount: 1,
		},
		"three pages all collected": {
			pages:         3,
			expectedCount: 3,
		},
		"stops at maxPagesLimit": {
			pages:         maxPagesLimit + 5,
			expectedCount: maxPagesLimit,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var requestCount atomic.Int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := int(requestCount.Add(1))
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				w.Header().Set("x-page", strconv.Itoa(page))
				w.Header().Set("x-total-pages", strconv.Itoa(tc.pages))
				jsonResponse(t, w, []map[string]any{{"page": float64(n)}})
			}))
			defer srv.Close()

			client := newTestGitLabClient(t, srv)
			items, err := client.listAllPages(t.Context(), "/api/v4/projects", "per_page=100")

			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, items, tc.expectedCount)
			assert.EqualValues(t, tc.expectedCount, requestCount.Load())
		})
	}
}

func TestListProjects(t *testing.T) {
	testCases := map[string]struct {
		handler       http.HandlerFunc
		expectedCount int
		expectErr     bool
	}{
		"success single page": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-page", "1")
				w.Header().Set("x-total-pages", "1")
				jsonResponse(t, w, []map[string]any{{"id": float64(1), "name": "repo"}})
			},
			expectedCount: 1,
		},
		"success multi page": {
			handler: func() http.HandlerFunc {
				var call atomic.Int32
				return func(w http.ResponseWriter, r *http.Request) {
					n := int(call.Add(1))
					w.Header().Set("x-page", strconv.Itoa(n))
					w.Header().Set("x-total-pages", "2")
					jsonResponse(t, w, []map[string]any{{"id": float64(n)}})
				}
			}(),
			expectedCount: 2,
		},
		"server error": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestGitLabClient(t, srv)
			items, err := client.listProjects(t.Context())

			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, items, tc.expectedCount)
		})
	}
}

func TestListPipelines(t *testing.T) {
	maxPagesLimit = 3
	testCases := map[string]struct {
		handler       http.HandlerFunc
		projectID     string
		expectedCount int
		expectErr     bool
	}{
		"success": {
			projectID: "42",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "/42/pipelines")
				w.Header().Set("x-page", "1")
				w.Header().Set("x-total-pages", "1")
				jsonResponse(t, w, []map[string]any{{"id": float64(10)}, {"id": float64(11)}})
			},
			expectedCount: 2,
		},
		"server error": {
			projectID: "42",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestGitLabClient(t, srv)
			items, err := client.listPipelines(t.Context(), tc.projectID)

			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, items, tc.expectedCount)
		})
	}
}
