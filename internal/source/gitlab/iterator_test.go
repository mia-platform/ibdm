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
			it := client.newPageIterator("/api/v4/projects")

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
		it := client.newPageIterator("/api/v4/groups")

		items, err := it.next(t.Context())
		require.NoError(t, err)
		require.Len(t, items, 1)

		_, err = it.next(t.Context())
		require.ErrorIs(t, err, ErrIteratorDone)

		_, err = it.next(t.Context())
		require.ErrorIs(t, err, ErrIteratorDone)
	})

	t.Run("newProjectsIterator hits /api/v4/projects", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v4/projects", r.URL.Path)
			w.Header().Set("x-total-pages", "1")
			jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
		}))
		defer srv.Close()

		client := newTestGitLabClient(t, srv)
		it := client.newProjectsIterator()

		items, err := it.next(t.Context())
		require.NoError(t, err)
		assert.Len(t, items, 1)
	})

	t.Run("newGroupsIterator hits /api/v4/groups", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v4/groups", r.URL.Path)
			w.Header().Set("x-total-pages", "1")
			jsonResponse(t, w, []map[string]any{{"id": float64(1)}})
		}))
		defer srv.Close()

		client := newTestGitLabClient(t, srv)
		it := client.newGroupsIterator()

		items, err := it.next(t.Context())
		require.NoError(t, err)
		assert.Len(t, items, 1)
	})
}

func TestPageIteratorWithResources(t *testing.T) {
	testCases := map[string]struct {
		handler       http.HandlerFunc
		path          string
		expectedCount int
		expectErr     bool
	}{
		"single page of pipelines": {
			path: "/api/v4/projects/42/pipelines",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "/42/pipelines")
				w.Header().Set("x-total-pages", "1")
				jsonResponse(t, w, []map[string]any{{"id": float64(10)}, {"id": float64(11)}})
			},
			expectedCount: 2,
		},
		"multi page": {
			path: "/api/v4/projects/7/pipelines",
			handler: func() http.HandlerFunc {
				var call atomic.Int32
				return func(w http.ResponseWriter, r *http.Request) {
					n := int(call.Add(1))
					w.Header().Set("x-total-pages", "2")
					jsonResponse(t, w, []map[string]any{{"id": float64(n * 10)}})
				}
			}(),
			expectedCount: 2,
		},
		"empty result": {
			path: "/api/v4/projects/99/pipelines",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-total-pages", "0")
				jsonResponse(t, w, []map[string]any{})
			},
			expectedCount: 0,
		},
		"single page of group access tokens": {
			path: "/api/v4/groups/42/access_tokens",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "/42/access_tokens")
				w.Header().Set("x-total-pages", "1")
				jsonResponse(t, w, []map[string]any{{"id": float64(1)}, {"id": float64(2)}})
			},
			expectedCount: 2,
		},
		"server error": {
			path: "/api/v4/projects/42/pipelines",
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
			it := client.newPageIterator(tc.path)

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
		it := client.newPageIterator("/api/v4/projects/1/pipelines")

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

func TestNewProjectResourcesIterator(t *testing.T) {
	client := &gitLabClient{}

	t.Run("pipeline resource", func(t *testing.T) {
		it, err := client.newProjectResourcesIterator(pipelineResource, "42")
		require.NoError(t, err)
		assert.NotNil(t, it)
	})

	t.Run("access token resource", func(t *testing.T) {
		it, err := client.newProjectResourcesIterator(accessTokenResource, "42")
		require.NoError(t, err)
		assert.NotNil(t, it)
	})

	t.Run("unknown resource returns error", func(t *testing.T) {
		it, err := client.newProjectResourcesIterator("unknown", "42")
		require.Error(t, err)
		assert.Nil(t, it)
		assert.Contains(t, err.Error(), "unknown project resource")
	})
}

func TestNewGroupResourcesIterator(t *testing.T) {
	client := &gitLabClient{}

	t.Run("access token resource", func(t *testing.T) {
		it, err := client.newGroupResourcesIterator(accessTokenResource, "10")
		require.NoError(t, err)
		assert.NotNil(t, it)
	})

	t.Run("unknown resource returns error", func(t *testing.T) {
		it, err := client.newGroupResourcesIterator("unknown", "10")
		require.Error(t, err)
		assert.Nil(t, it)
		assert.Contains(t, err.Error(), "unknown group resource")
	})
}
