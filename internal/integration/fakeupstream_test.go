// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	// routesFile is the name of the route table of an upstream fixture directory.
	routesFile = "routes.yaml"
	// baseURLPlaceholder is replaced with the URL of the fake in bodies and headers, for sources
	// that follow absolute URLs found in the responses.
	baseURLPlaceholder = "{{.BaseURL}}"
)

// route is one answer of a fake upstream, as declared in a routes.yaml file.
type route struct {
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
	// Query, when set, must all be present in the request with these values. Other parameters
	// are allowed, so that one route can answer every page size.
	Query map[string]string `yaml:"query"`
	// RequestHeaders, when set, must all be present in the request with these values, for
	// upstreams whose answer depends on a header such as an API version.
	RequestHeaders map[string]string `yaml:"requestHeaders"`
	// BodyContains, when set, must be a substring of the request body, for upstreams that page
	// through a body parameter, such as the OFFSET of a query.
	BodyContains string `yaml:"bodyContains"`
	// Status defaults to 200.
	Status  int               `yaml:"status"`
	Headers map[string]string `yaml:"headers"`
	// Body is a file next to routes.yaml, sent as application/json. Empty means no body.
	Body string `yaml:"body"`

	body []byte
}

// routeTable is the content of a routes.yaml file.
type routeTable struct {
	Routes []route `yaml:"routes"`
}

// recordedRequest is a request a fake upstream received.
type recordedRequest struct {
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Query  string            `json:"query,omitempty"`
	Header map[string]string `json:"header,omitempty"`
	Body   string            `json:"body,omitempty"`
}

// fakeUpstream answers the requests of an ibdm source from fixture files and records them. It is
// strict: a request no route matches gets a 404 and fails the test.
type fakeUpstream struct {
	server *httptest.Server
	routes []route
	// recordedHeaders are the request headers kept in the record, in canonical form.
	recordedHeaders []string

	mu         sync.Mutex
	requests   []recordedRequest
	unexpected []string
}

// newFakeUpstream starts a fake upstream answering with routes. The headers named in
// recordedHeaders are kept in the record of each request. The test cleanup stops the fake and
// fails the test if a request matched no route.
func newFakeUpstream(t *testing.T, routes []route, recordedHeaders ...string) *fakeUpstream {
	t.Helper()

	upstream := startFakeUpstream(routes, recordedHeaders...)
	t.Cleanup(func() {
		upstream.server.Close()
		require.Empty(t, upstream.unexpectedRequests(), "requests the fake upstream has no route for")
	})
	return upstream
}

// startFakeUpstream starts a fake upstream that the caller stops.
func startFakeUpstream(routes []route, recordedHeaders ...string) *fakeUpstream {
	upstream := &fakeUpstream{routes: routes}
	for _, header := range recordedHeaders {
		upstream.recordedHeaders = append(upstream.recordedHeaders, http.CanonicalHeaderKey(header))
	}
	// Assigned before the start, so that the handler can read the URL without a race.
	upstream.server = httptest.NewUnstartedServer(http.HandlerFunc(upstream.handle))
	upstream.server.Start()
	return upstream
}

// loadRoutes reads the routes.yaml file of dir and the bodies it names.
func loadRoutes(t *testing.T, dir string) []route {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(dir, routesFile))
	require.NoError(t, err)

	var table routeTable
	decoder := yaml.NewDecoder(strings.NewReader(string(content)))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&table), "decoding %s", filepath.Join(dir, routesFile))

	for i := range table.Routes {
		if table.Routes[i].Body == "" {
			continue
		}
		table.Routes[i].body, err = os.ReadFile(filepath.Join(dir, table.Routes[i].Body))
		require.NoError(t, err)
	}
	return table.Routes
}

// baseURL is the URL of the fake, to point a source at it.
func (u *fakeUpstream) baseURL() string {
	return u.server.URL
}

// received returns a copy of the requests received so far, in arrival order.
func (u *fakeUpstream) received() []recordedRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]recordedRequest(nil), u.requests...)
}

// calls counts the requests received for method and path.
func (u *fakeUpstream) calls(method, path string) int {
	count := 0
	for _, request := range u.received() {
		if request.Method == method && request.Path == path {
			count++
		}
	}
	return count
}

// unexpectedRequests returns the requests no route matched.
func (u *fakeUpstream) unexpectedRequests() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.unexpected...)
}

// handle records the request and answers it with the first matching route.
func (u *fakeUpstream) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		u.recordUnexpected("unreadable body: " + err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	u.record(r, string(body))

	matched, ok := u.match(r, string(body))
	if !ok {
		u.recordUnexpected(r.Method + " " + r.URL.String())
		http.NotFound(w, r)
		return
	}

	for key, value := range matched.Headers {
		w.Header().Set(key, u.expand(value))
	}
	if len(matched.body) > 0 {
		w.Header().Set("Content-Type", "application/json")
	}

	status := matched.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if len(matched.body) > 0 {
		// A failed write surfaces as an error of the source under test.
		_, _ = w.Write([]byte(u.expand(string(matched.body))))
	}
}

// match returns the first route answering r.
func (u *fakeUpstream) match(r *http.Request, body string) (route, bool) {
	query := r.URL.Query()
	for _, candidate := range u.routes {
		if candidate.Method != r.Method || candidate.Path != r.URL.Path {
			continue
		}
		if queryMatches(candidate.Query, query) && headersMatch(candidate.RequestHeaders, r.Header) && strings.Contains(body, candidate.BodyContains) {
			return candidate, true
		}
	}
	return route{}, false
}

// queryMatches reports whether every parameter of want has its value in got.
func queryMatches(want map[string]string, got url.Values) bool {
	for key, value := range want {
		if got.Get(key) != value {
			return false
		}
	}
	return true
}

// headersMatch reports whether every header of want has its value in got.
func headersMatch(want map[string]string, got http.Header) bool {
	for key, value := range want {
		if got.Get(key) != value {
			return false
		}
	}
	return true
}

// expand replaces the base URL placeholder with the URL of the fake.
func (u *fakeUpstream) expand(value string) string {
	return strings.ReplaceAll(value, baseURLPlaceholder, u.server.URL)
}

// record stores the request, with its query sorted, its recorded headers and its body.
func (u *fakeUpstream) record(r *http.Request, body string) {
	request := recordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.Query().Encode(),
		Body:   body,
	}
	for _, header := range u.recordedHeaders {
		if value := r.Header.Get(header); value != "" {
			if request.Header == nil {
				request.Header = make(map[string]string)
			}
			request.Header[header] = value
		}
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	u.requests = append(u.requests, request)
}

// recordUnexpected stores the description of a request no route matched.
func (u *fakeUpstream) recordUnexpected(description string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.unexpected = append(u.unexpected, description)
}
