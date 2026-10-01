// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// catalogPath is the path the fake Catalog accepts items on. The Catalog destination posts every
// upsert and delete to exactly MIA_CATALOG_ENDPOINT.
const catalogPath = "/items"

// catalogFailure makes the fake Catalog answer status to the items match selects.
type catalogFailure struct {
	match  func(item map[string]any) bool
	status int
}

// fakeCatalog records every item the Catalog destination of ibdm sends. It answers 204, the only
// status the destination treats as success, unless a failure matches the item.
type fakeCatalog struct {
	server *httptest.Server

	mu         sync.Mutex
	items      []map[string]any
	failures   []catalogFailure
	unexpected []string
}

// newFakeCatalog starts a fake Catalog. The test cleanup stops it and fails the test if it
// received a request it does not understand.
func newFakeCatalog(t *testing.T) *fakeCatalog {
	t.Helper()

	catalog := startFakeCatalog()
	t.Cleanup(func() {
		catalog.server.Close()
		require.Empty(t, catalog.unexpectedRequests(), "unexpected requests to the fake Catalog")
	})
	return catalog
}

// startFakeCatalog starts a fake Catalog that the caller stops.
func startFakeCatalog() *fakeCatalog {
	catalog := new(fakeCatalog)
	catalog.server = httptest.NewServer(http.HandlerFunc(catalog.handle))
	return catalog
}

// endpoint is the value of MIA_CATALOG_ENDPOINT for the fake.
func (c *fakeCatalog) endpoint() string {
	return c.server.URL + catalogPath
}

// env returns the environment that points the Catalog destination at the fake, with no
// authentication.
func (c *fakeCatalog) env() map[string]string {
	return map[string]string{"MIA_CATALOG_ENDPOINT": c.endpoint()}
}

// failOn makes the fake answer status, with a JSON error message, to every item match selects.
// Those items are recorded anyway.
func (c *fakeCatalog) failOn(match func(item map[string]any) bool, status int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = append(c.failures, catalogFailure{match: match, status: status})
}

// received returns a copy of the items received so far, in arrival order.
func (c *fakeCatalog) received() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]map[string]any(nil), c.items...)
}

// waitFor waits until at least count items have arrived, and returns them. Webhook processing
// is asynchronous: ibdm answers the webhook before it sends the items.
func (c *fakeCatalog) waitFor(t *testing.T, count int, timeout time.Duration) []map[string]any {
	t.Helper()

	require.Eventually(t, func() bool {
		return len(c.received()) >= count
	}, timeout, pollInterval, "the fake Catalog did not receive %d items within %s", count, timeout)
	return c.received()
}

// unexpectedRequests returns the requests the fake could not record as items.
func (c *fakeCatalog) unexpectedRequests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.unexpected...)
}

// handle records one item, or the reason the request is not one.
func (c *fakeCatalog) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != catalogPath {
		c.recordUnexpected(r.Method + " " + r.URL.String())
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var item map[string]any
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		c.recordUnexpected("undecodable body: " + err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	status := c.record(item)
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A failed write only loses the error message, which the test does not need.
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "fake Catalog failure"})
}

// record stores item and returns the status to answer.
func (c *fakeCatalog) record(item map[string]any) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = append(c.items, item)
	for _, failure := range c.failures {
		if failure.match(item) {
			return failure.status
		}
	}
	return http.StatusNoContent
}

// recordUnexpected stores the description of a request the fake does not understand.
func (c *fakeCatalog) recordUnexpected(description string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unexpected = append(c.unexpected, description)
}
