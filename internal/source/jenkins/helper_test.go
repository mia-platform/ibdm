// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	testUsername = "my-user"
	testAPIToken = "my-api-token"
)

// testFixedTime is the canonical fixed time used across all time-sensitive tests.
var testFixedTime = time.Date(2026, time.January, 15, 10, 0, 0, 0, time.UTC)

func init() {
	timeSource = func() time.Time { return testFixedTime }
}

// fakeResponse is the canned response of a fake Jenkins route.
type fakeResponse struct {
	status  int
	body    string
	headers map[string]string
}

// fakeJenkins serves canned responses keyed by escaped path, optionally followed by
// "?tree=" and the requested tree, and records every requested path. Requests without the
// expected basic auth credentials are rejected.
type fakeJenkins struct {
	mu       sync.Mutex
	routes   map[string]fakeResponse
	requests []string
	uris     []string
}

func newFakeJenkins(routes map[string]fakeResponse) *fakeJenkins {
	return &fakeJenkins{routes: routes}
}

func (f *fakeJenkins) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, password, ok := r.BasicAuth()
	if !ok || user != testUsername || password != testAPIToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	path := r.URL.EscapedPath()
	f.mu.Lock()
	f.requests = append(f.requests, path)
	f.uris = append(f.uris, r.URL.RequestURI())
	f.mu.Unlock()

	response, found := f.routes[path+"?tree="+r.URL.Query().Get(treeQueryParam)]
	if !found {
		response, found = f.routes[path]
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	for key, value := range response.headers {
		w.Header().Set(key, value)
	}
	w.Header().Set("Content-Type", "application/json")
	status := response.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(response.body))
}

// requested returns the requested paths, in order.
func (f *fakeJenkins) requested() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

// requestedURIs returns the requested URIs, with their query, in order.
func (f *fakeJenkins) requestedURIs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.uris)
}

// newTestSource returns a Source whose client targets a test server serving handler.
func newTestSource(t *testing.T, handler http.Handler) *Source {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &Source{
		client: &client{
			baseURL:    server.URL,
			username:   testUsername,
			apiToken:   testAPIToken,
			httpClient: &http.Client{Timeout: 5 * time.Second},
		},
	}
}

// collectData drains a closed source.Data channel into a slice.
func collectData(t *testing.T, ch <-chan source.Data) []source.Data {
	t.Helper()
	var result []source.Data
	for d := range ch {
		result = append(result, d)
	}
	return result
}

// byType groups the collected data by type.
func byType(t *testing.T, data []source.Data) map[string][]source.Data {
	t.Helper()
	out := make(map[string][]source.Data)
	for _, d := range data {
		out[d.Type] = append(out[d.Type], d)
	}
	return out
}

// runSync runs a sync against routes for the requested types and returns the emitted data, the
// requested paths and the returned error.
func runSync(t *testing.T, routes map[string]fakeResponse, types ...string) ([]source.Data, []string, error) {
	t.Helper()

	fake := newFakeJenkins(routes)
	src := newTestSource(t, fake)

	typesToSync := make(map[string]source.MappingExtras, len(types))
	for _, dataType := range types {
		typesToSync[dataType] = source.MappingExtras{}
	}

	results := make(chan source.Data, 100)
	err := src.StartSyncProcess(t.Context(), typesToSync, results)
	close(results)

	return collectData(t, results), fake.requested(), err
}

// withRoutes returns a copy of base with overrides applied.
func withRoutes(base, overrides map[string]fakeResponse) map[string]fakeResponse {
	out := maps.Clone(base)
	maps.Copy(out, overrides)
	return out
}
