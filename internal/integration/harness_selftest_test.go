// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selftestUpstreamDir holds the fixtures of the harness self-tests.
const selftestUpstreamDir = "testdata/upstream/selftest"

// doRequest sends a request with body, or none, and returns the status and the response body.
func doRequest(t *testing.T, method, url string, body []byte, header http.Header) (int, string, http.Header) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, bytes.NewReader(body))
	require.NoError(t, err)
	for key, values := range header {
		req.Header[key] = values
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(content), resp.Header
}

// postItem sends item to the fake Catalog as the Catalog destination does, and returns the status.
func postItem(t *testing.T, catalog *fakeCatalog, item map[string]any) int {
	t.Helper()

	body, err := json.Marshal(item)
	require.NoError(t, err)
	status, _, _ := doRequest(t, http.MethodPost, catalog.endpoint(), body, http.Header{"Content-Type": {"application/json"}})
	return status
}

// TestFakeUpstream proves the routing, the placeholders, the record and the strictness of the
// fake upstream.
func TestFakeUpstream(t *testing.T) {
	t.Parallel()

	upstream := startFakeUpstream(loadRoutes(t, selftestUpstreamDir), "X-Widget-Version")
	defer upstream.server.Close()

	status, body, header := doRequest(t, http.MethodGet, upstream.baseURL()+"/widgets?per_page=10", nil, http.Header{"X-Widget-Version": {"2026-01-01"}})
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{"widgets": [{"name": "gizmo", "next": "`+upstream.baseURL()+`/widgets?page=2"}]}`, body, "the base URL placeholder is expanded in bodies")
	assert.Equal(t, `<`+upstream.baseURL()+`/widgets?page=2>; rel="next"`, header.Get("Link"), "and in headers")
	assert.Equal(t, "application/json", header.Get("Content-Type"))

	status, body, _ = doRequest(t, http.MethodGet, upstream.baseURL()+"/widgets?page=2&per_page=10", nil, nil)
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{"widgets": [{"name": "sprocket"}]}`, body, "a query route matches when its parameters are present, whatever else the request carries")

	status, body, _ = doRequest(t, http.MethodDelete, upstream.baseURL()+"/widgets/gizmo", nil, nil)
	assert.Equal(t, http.StatusNoContent, status, "the declared status is used")
	assert.Empty(t, body)

	assert.Empty(t, upstream.unexpectedRequests())

	status, _, _ = doRequest(t, http.MethodGet, upstream.baseURL()+"/gadgets", nil, nil)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, []string{"GET /gadgets"}, upstream.unexpectedRequests(), "a request with no route is reported")

	assert.Equal(t, []recordedRequest{
		{Method: http.MethodGet, Path: "/widgets", Query: "per_page=10", Header: map[string]string{"X-Widget-Version": "2026-01-01"}},
		{Method: http.MethodGet, Path: "/widgets", Query: "page=2&per_page=10"},
		{Method: http.MethodDelete, Path: "/widgets/gizmo"},
		{Method: http.MethodGet, Path: "/gadgets"},
	}, upstream.received(), "every request is recorded, with its sorted query and the recorded headers only")
	assert.Equal(t, 2, upstream.calls(http.MethodGet, "/widgets"))
}

// TestFakeCatalog proves the record, the failure injection, the wait and the strictness of the
// fake Catalog.
func TestFakeCatalog(t *testing.T) {
	t.Parallel()

	catalog := startFakeCatalog()
	defer catalog.server.Close()
	catalog.failOn(func(item map[string]any) bool { return item["name"] == "broken-widget" }, http.StatusInternalServerError)

	assert.Equal(t, http.StatusNoContent, postItem(t, catalog, map[string]any{"name": "gizmo", "operation": "upsert"}))
	assert.Equal(t, http.StatusInternalServerError, postItem(t, catalog, map[string]any{"name": "broken-widget", "operation": "upsert"}))

	// A late item, as a webhook produces: the post runs off the test goroutine, so it cannot use
	// require, and a failure shows as the wait timing out.
	late, err := http.NewRequestWithContext(t.Context(), http.MethodPost, catalog.endpoint(), strings.NewReader(`{"name": "sprocket", "operation": "delete"}`))
	require.NoError(t, err)
	go func() {
		time.Sleep(100 * time.Millisecond)
		if resp, err := http.DefaultClient.Do(late); err == nil {
			resp.Body.Close()
		}
	}()
	items := catalog.waitFor(t, 3, 5*time.Second)
	assert.Equal(t, []map[string]any{
		{"name": "gizmo", "operation": "upsert"},
		{"name": "broken-widget", "operation": "upsert"},
		{"name": "sprocket", "operation": "delete"},
	}, items, "failed items are recorded too, in arrival order")
	assert.Empty(t, catalog.unexpectedRequests())

	status, _, _ := doRequest(t, http.MethodGet, catalog.endpoint(), nil, nil)
	assert.Equal(t, http.StatusNotFound, status)
	status, _, _ = doRequest(t, http.MethodPost, catalog.endpoint(), []byte("not json"), nil)
	assert.Equal(t, http.StatusBadRequest, status)
	require.Len(t, catalog.unexpectedRequests(), 2, "a request that is not an item is reported")
	assert.Equal(t, "GET "+catalogPath, catalog.unexpectedRequests()[0])
}

// TestGoldenNormalisation proves that the golden content does not depend on arrival order, times
// or the addresses of the fakes.
func TestGoldenNormalisation(t *testing.T) {
	t.Parallel()

	catalog := newFakeCatalog(t)
	upstream := newFakeUpstream(t, loadRoutes(t, selftestUpstreamDir), "X-Widget-Version")

	items := []map[string]any{
		{"apiVersion": "integration.example.com/v1", "itemFamily": "widgets", "name": "sprocket", "operation": "upsert", "operationTime": "2026-10-01T10:00:00Z", "data": map[string]any{"self": upstream.baseURL() + "/widgets/sprocket", "parts": []any{"catalog " + catalog.endpoint()}}},
		{"apiVersion": "integration.example.com/v1", "itemFamily": "widgets", "name": "gizmo", "operation": "delete", "operationTime": "2026-10-01T10:00:01Z"},
		{"apiVersion": "integration.example.com/v1", "itemFamily": "widgets", "name": "gizmo", "operation": "upsert", "operationTime": "2026-10-01T10:00:02Z"},
	}
	for _, item := range items {
		require.Equal(t, http.StatusNoContent, postItem(t, catalog, item))
	}
	doRequest(t, http.MethodGet, upstream.baseURL()+"/widgets?page=2", nil, nil)
	doRequest(t, http.MethodGet, upstream.baseURL()+"/widgets", nil, http.Header{"X-Widget-Version": {upstream.baseURL()}})

	assertGolden(t, "selftest/normalisation", catalog, upstream)
	assert.Equal(t, upstream.baseURL(), upstream.received()[1].Header["X-Widget-Version"], "normalising leaves the record of the fake untouched")
	assert.True(t, strings.HasPrefix(catalog.received()[0]["operationTime"].(string), "2026-10-01"), "and the received items too")
}
