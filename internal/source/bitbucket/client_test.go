// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListWorkspacesURL(t *testing.T) {
	t.Parallel()

	c := &client{baseURL: "https://api.bitbucket.org"}
	it := c.listWorkspaces()
	pi, ok := it.(*pageIterator)
	require.True(t, ok)
	assert.Equal(t, "https://api.bitbucket.org/2.0/user/workspaces?pagelen=100", pi.nextURL)
}

func TestListRepositoriesURL(t *testing.T) {
	t.Parallel()

	c := &client{baseURL: "https://api.bitbucket.org"}
	it := c.listRepositories("my-workspace")
	pi, ok := it.(*pageIterator)
	require.True(t, ok)
	assert.Equal(t, "https://api.bitbucket.org/2.0/repositories/my-workspace?pagelen=100", pi.nextURL)
}

func TestListRepositoriesURLEscapesSlug(t *testing.T) {
	t.Parallel()

	c := &client{baseURL: "https://api.bitbucket.org"}
	it := c.listRepositories("my workspace")
	pi, ok := it.(*pageIterator)
	require.True(t, ok)
	assert.Contains(t, pi.nextURL, "my%20workspace")
	assert.Contains(t, pi.nextURL, "pagelen=100")
}

func TestListPipelinesURL(t *testing.T) {
	t.Parallel()

	c := &client{baseURL: "https://api.bitbucket.org"}
	it := c.listPipelines("my-workspace", "my-repo")
	pi, ok := it.(*pageIterator)
	require.True(t, ok)
	assert.Equal(t, "https://api.bitbucket.org/2.0/repositories/my-workspace/my-repo/pipelines", pi.nextURL)
	assert.NotContains(t, pi.nextURL, "pagelen")
}

func TestListPipelinesURLEscapesSlugs(t *testing.T) {
	t.Parallel()

	c := &client{baseURL: "https://api.bitbucket.org"}
	it := c.listPipelines("my workspace", "my repo")
	pi, ok := it.(*pageIterator)
	require.True(t, ok)
	assert.Contains(t, pi.nextURL, "my%20workspace")
	assert.Contains(t, pi.nextURL, "my%20repo")
}

func TestGetRepository(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		handler     http.HandlerFunc
		expectRepo  map[string]any
		errContains string
	}{
		"successful fetch": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/2.0/repositories/ws/repo", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"full_name": "ws/repo",
					"slug":      "repo",
				})
			},
			expectRepo: map[string]any{
				"full_name": "ws/repo",
				"slug":      "repo",
			},
		},
		"server error": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error":"not found"}`))
			},
			errContains: "unexpected status 404",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(tc.handler)
			t.Cleanup(server.Close)

			c := &client{
				baseURL:     server.URL,
				accessToken: "test-token",
				httpClient:  server.Client(),
			}

			repo, err := c.getRepository(t.Context(), "ws", "repo")
			if tc.errContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expectRepo, repo)
		})
	}
}

func TestDoRequestBearerAuth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:     server.URL,
		accessToken: "test-token",
		httpClient:  server.Client(),
	}

	resp, err := c.doRequest(t.Context(), server.URL+"/test")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDoRequestBasicAuth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		require.True(t, ok)
		assert.Equal(t, "myuser", username)
		assert.Equal(t, "mytoken", password)
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:     server.URL,
		apiUsername: "myuser",
		apiToken:    "mytoken",
		httpClient:  server.Client(),
	}

	resp, err := c.doRequest(t.Context(), server.URL+"/test")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestGetRepositoryDecodeError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`))
	}))
	t.Cleanup(server.Close)

	c := &client{
		baseURL:     server.URL,
		accessToken: "test-token",
		httpClient:  server.Client(),
	}

	_, err := c.getRepository(t.Context(), "ws", "repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode repository response")
}
