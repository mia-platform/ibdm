// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakePageableRequest(t *testing.T) {
	testCases := map[string]struct {
		handler       http.HandlerFunc
		expectedItems []map[string]any
		expectedTotal int
		expectErr     bool
	}{
		"successful single page": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "test-token", r.Header.Get("PRIVATE-TOKEN"))
				assert.Equal(t, "application/json", r.Header.Get("Accept"))
				assert.Equal(t, "1", r.URL.Query().Get("page"))
				w.Header().Set("x-total-pages", "1")
				jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
			},
			expectedItems: []map[string]any{{"id": float64(1)}},
			expectedTotal: 1,
		},
		"pagination headers parsed": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-total-pages", "5")
				jsonResponse(t, w, []map[string]any{{"id": float64(2)}})
			},
			expectedItems: []map[string]any{{"id": float64(2)}},
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
			items, totalPages, err := client.makePageableRequest(t.Context(), "/api/v4/projects", 1)

			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedItems, items)
			assert.Equal(t, tc.expectedTotal, totalPages)
		})
	}
}

func TestMakeRequestList(t *testing.T) {
	testCases := map[string]struct {
		handler       http.HandlerFunc
		expectedItems []map[string]any
		expectErr     bool
		expectErrIs   error
	}{
		"success": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				jsonResponse(t, w, []map[string]any{{"id": float64(1)}, {"id": float64(2)}})
			},
			expectedItems: []map[string]any{{"id": float64(1)}, {"id": float64(2)}},
		},
		"empty list": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				jsonResponse(t, w, []map[string]any{})
			},
			expectedItems: []map[string]any{},
		},
		"non-200 status": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectErr: true,
		},
		"unauthorized returns ErrNotAccessible": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			expectErr:   true,
			expectErrIs: ErrNotAccessible,
		},
		"forbidden returns ErrNotAccessible": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			},
			expectErr:   true,
			expectErrIs: ErrNotAccessible,
		},
		"malformed JSON": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
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
			items, err := client.makeRequestList(t.Context(), "/api/v4/projects/1/access_tokens", "")

			if tc.expectErr {
				require.Error(t, err)
				if tc.expectErrIs != nil {
					require.ErrorIs(t, err, tc.expectErrIs)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedItems, items)
		})
	}
}

func TestListFactoryMethods(t *testing.T) {
	t.Run("listProjects hits /api/v4/projects", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v4/projects", r.URL.Path)
			w.Header().Set("x-total-pages", "1")
			jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
		}))
		defer srv.Close()

		client := newTestGitLabClient(t, srv)
		it := client.listProjects()
		items, err := it.next(t.Context())
		require.NoError(t, err)
		assert.Len(t, items, 1)
	})

	t.Run("listGroups hits /api/v4/groups", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v4/groups", r.URL.Path)
			w.Header().Set("x-total-pages", "1")
			jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
		}))
		defer srv.Close()

		client := newTestGitLabClient(t, srv)
		it := client.listGroups()
		items, err := it.next(t.Context())
		require.NoError(t, err)
		assert.Len(t, items, 1)
	})

	t.Run("listProjectPipelines hits correct path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v4/projects/42/pipelines", r.URL.Path)
			w.Header().Set("x-total-pages", "1")
			jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
		}))
		defer srv.Close()

		client := newTestGitLabClient(t, srv)
		it := client.listProjectPipelines("42")
		items, err := it.next(t.Context())
		require.NoError(t, err)
		assert.Len(t, items, 1)
	})

	t.Run("listProjectAccessTokens hits correct path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v4/projects/42/access_tokens", r.URL.Path)
			w.Header().Set("x-total-pages", "1")
			jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
		}))
		defer srv.Close()

		client := newTestGitLabClient(t, srv)
		it := client.listProjectAccessTokens("42")
		items, err := it.next(t.Context())
		require.NoError(t, err)
		assert.Len(t, items, 1)
	})

	t.Run("listGroupAccessTokens hits correct path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v4/groups/10/access_tokens", r.URL.Path)
			w.Header().Set("x-total-pages", "1")
			jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
		}))
		defer srv.Close()

		client := newTestGitLabClient(t, srv)
		it := client.listGroupAccessTokens("10")
		items, err := it.next(t.Context())
		require.NoError(t, err)
		assert.Len(t, items, 1)
	})
}
