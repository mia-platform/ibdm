// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestGitLabClient returns a gitLabClient pointing at the provided test server.
func newTestGitLabClient(t *testing.T, srv *httptest.Server) *gitLabClient {
	t.Helper()
	return &gitLabClient{
		config: sourceConfig{
			Token:   "test-token",
			BaseURL: srv.URL,
		},
		http: srv.Client(),
	}
}

// jsonResponse writes a JSON-encoded value with status 200 to w.
func jsonResponse(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

// mustMarshal marshals v to JSON, failing the test on error.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// paginatedHandler returns an http.HandlerFunc that serves pre-canned JSON arrays
// for the given path map. Responses always include x-page:1 / x-total-pages:1.
func paginatedHandler(t *testing.T, routes map[string]any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		data, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("x-page", "1")
		w.Header().Set("x-total-pages", "1")
		jsonResponse(t, w, data)
	}
}
