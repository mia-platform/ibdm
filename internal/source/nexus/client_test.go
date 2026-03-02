// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, handler http.Handler) *client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	return &client{
		baseURL:       u,
		tokenName:     "test-token",
		tokenPasscode: "test-passcode",
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		config    config
		expectErr bool
	}{
		"valid config": {
			config: config{
				URL:           "https://nexus.example.com",
				TokenName:     "mytoken",
				TokenPasscode: "secret",
				HTTPTimeout:   30 * time.Second,
			},
		},
		"invalid URL": {
			config: config{
				URL:           "://invalid",
				TokenName:     "mytoken",
				TokenPasscode: "secret",
				HTTPTimeout:   30 * time.Second,
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, err := newClient(tc.config)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.config.TokenName, c.tokenName)
			assert.Equal(t, tc.config.TokenPasscode, c.tokenPasscode)
			assert.Equal(t, tc.config.URL, c.baseURL.String())
		})
	}
}

func TestDoRequest(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		handler      http.HandlerFunc
		path         string
		queryParams  url.Values
		expectErr    bool
		expectStatus int
		validateAuth bool
	}{
		"successful request with basic auth": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				user, pass, ok := r.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "test-token", user)
				assert.Equal(t, "test-passcode", pass)
				assert.Equal(t, "application/json", r.Header.Get("Accept"))
				w.WriteHeader(http.StatusOK)
			},
			path:         "/v1/repositories",
			expectStatus: http.StatusOK,
		},
		"request with query parameters": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "maven-central", r.URL.Query().Get("repository"))
				assert.Equal(t, "abc123", r.URL.Query().Get("continuationToken"))
				w.WriteHeader(http.StatusOK)
			},
			path: "/v1/components",
			queryParams: url.Values{
				"repository":        {"maven-central"},
				"continuationToken": {"abc123"},
			},
			expectStatus: http.StatusOK,
		},
		"request uses service/rest base path": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/service/rest/v1/repositories", r.URL.Path)
				w.WriteHeader(http.StatusOK)
			},
			path:         "/v1/repositories",
			expectStatus: http.StatusOK,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := testClient(t, tc.handler)

			resp, err := c.doRequest(t.Context(), tc.path, tc.queryParams)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, tc.expectStatus, resp.StatusCode)
		})
	}
}

func TestListRepositories(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		handler     http.HandlerFunc
		expectErr   bool
		expectCount int
		expectFirst string
	}{
		"successful response with two repositories": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/service/rest/v1/repositories", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				repos := []map[string]any{
					{"name": "maven-central", "format": "maven2", "type": "proxy", "url": "https://nexus.example.com/repository/maven-central", "attributes": map[string]any{}},
					{"name": "docker-hosted", "format": "docker", "type": "hosted", "url": "https://nexus.example.com/repository/docker-hosted", "attributes": map[string]any{}},
				}
				require.NoError(t, json.NewEncoder(w).Encode(repos))
			},
			expectCount: 2,
			expectFirst: "maven-central",
		},
		"empty repository list": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				require.NoError(t, json.NewEncoder(w).Encode([]map[string]any{}))
			},
			expectCount: 0,
		},
		"server error": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, err := w.Write([]byte("internal error"))
				require.NoError(t, err)
			},
			expectErr: true,
		},
		"unauthorized": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, err := w.Write([]byte("unauthorized"))
				require.NoError(t, err)
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := testClient(t, tc.handler)

			repos, err := c.listRepositories(t.Context())
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, repos, tc.expectCount)
			if tc.expectFirst != "" && len(repos) > 0 {
				assert.Equal(t, tc.expectFirst, repos[0]["name"])
			}
		})
	}
}

func TestGetRepository(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		handler    http.HandlerFunc
		repoName   string
		expectErr  bool
		expectName string
	}{
		"successful response": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/service/rest/v1/repositories/maven-central", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				repo := map[string]any{
					"name": "maven-central", "format": "maven2", "type": "proxy",
					"url": "https://nexus.example.com/repository/maven-central", "attributes": map[string]any{},
				}
				require.NoError(t, json.NewEncoder(w).Encode(repo))
			},
			repoName:   "maven-central",
			expectName: "maven-central",
		},
		"repository not found": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, err := w.Write([]byte("not found"))
				require.NoError(t, err)
			},
			repoName:  "nonexistent",
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := testClient(t, tc.handler)

			repo, err := c.getRepository(t.Context(), tc.repoName)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectName, repo["name"])
		})
	}
}

func TestListComponentsPage(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		handler           http.HandlerFunc
		repository        string
		continuationToken string
		expectErr         bool
		expectItemCount   int
		expectNextToken   string
	}{
		"first page with continuation": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/service/rest/v1/components", r.URL.Path)
				require.Equal(t, "maven-central", r.URL.Query().Get("repository"))
				require.Empty(t, r.URL.Query().Get("continuationToken"))

				w.Header().Set("Content-Type", "application/json")
				token := "next-page-token"
				page := componentsPageResponse{
					Items: []map[string]any{
						{"id": "comp1", "name": "commons-lang3", "version": "3.14.0", "repository": "maven-central", "format": "maven2"},
					},
					ContinuationToken: &token,
				}
				require.NoError(t, json.NewEncoder(w).Encode(page))
			},
			repository:      "maven-central",
			expectItemCount: 1,
			expectNextToken: "next-page-token",
		},
		"last page without continuation": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				page := componentsPageResponse{
					Items:             []map[string]any{{"id": "comp2", "name": "guava", "version": "33.0"}},
					ContinuationToken: nil,
				}
				require.NoError(t, json.NewEncoder(w).Encode(page))
			},
			repository:      "maven-central",
			expectItemCount: 1,
		},
		"page with continuation token in request": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "abc123", r.URL.Query().Get("continuationToken"))
				w.Header().Set("Content-Type", "application/json")
				page := componentsPageResponse{
					Items:             []map[string]any{{"id": "comp3"}},
					ContinuationToken: nil,
				}
				require.NoError(t, json.NewEncoder(w).Encode(page))
			},
			repository:        "maven-central",
			continuationToken: "abc123",
			expectItemCount:   1,
		},
		"server error": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, err := w.Write([]byte("server error"))
				require.NoError(t, err)
			},
			repository: "maven-central",
			expectErr:  true,
		},
		"forbidden": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, err := w.Write([]byte("insufficient permissions"))
				require.NoError(t, err)
			},
			repository: "maven-central",
			expectErr:  true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := testClient(t, tc.handler)

			page, err := c.listComponentsPage(t.Context(), tc.repository, tc.continuationToken)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, page.Items, tc.expectItemCount)
			if tc.expectNextToken != "" {
				require.NotNil(t, page.ContinuationToken)
				assert.Equal(t, tc.expectNextToken, *page.ContinuationToken)
			} else {
				assert.Nil(t, page.ContinuationToken)
			}
		})
	}
}

func TestListComponentsPagination(t *testing.T) {
	t.Parallel()

	pageCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch pageCount {
		case 0:
			require.Empty(t, r.URL.Query().Get("continuationToken"))
			token := "page2"
			page := componentsPageResponse{
				Items:             []map[string]any{{"id": "comp1"}},
				ContinuationToken: &token,
			}
			require.NoError(t, json.NewEncoder(w).Encode(page))
		case 1:
			require.Equal(t, "page2", r.URL.Query().Get("continuationToken"))
			token := "page3"
			page := componentsPageResponse{
				Items:             []map[string]any{{"id": "comp2"}},
				ContinuationToken: &token,
			}
			require.NoError(t, json.NewEncoder(w).Encode(page))
		case 2:
			require.Equal(t, "page3", r.URL.Query().Get("continuationToken"))
			page := componentsPageResponse{
				Items:             []map[string]any{{"id": "comp3"}},
				ContinuationToken: nil,
			}
			require.NoError(t, json.NewEncoder(w).Encode(page))
		default:
			t.Fatal("unexpected page request")
		}

		pageCount++
	})

	c := testClient(t, handler)

	var allItems []map[string]any
	continuationToken := ""
	for {
		page, err := c.listComponentsPage(t.Context(), "maven-central", continuationToken)
		require.NoError(t, err)
		allItems = append(allItems, page.Items...)

		if page.ContinuationToken == nil || *page.ContinuationToken == "" {
			break
		}
		continuationToken = *page.ContinuationToken
	}

	assert.Len(t, allItems, 3)
	assert.Equal(t, "comp1", allItems[0]["id"])
	assert.Equal(t, "comp2", allItems[1]["id"])
	assert.Equal(t, "comp3", allItems[2]["id"])
}

func TestContextCancellation(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode([]map[string]any{}))
	})

	c := testClient(t, handler)
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	_, err := c.listRepositories(ctx)
	require.Error(t, err)
}
