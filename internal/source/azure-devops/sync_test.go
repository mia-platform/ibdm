// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"time"
)

func init() {
	timeSource = func() time.Time {
		return time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	}
}

// func testServer(t *testing.T) *httptest.Server {
// 	t.Helper()

// 	server := httptest.NewServer(nil)
// 	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		assert.Fail(t, "unexpected remote call", "request", r.RequestURI, "method", r.Method)
// 		http.NotFound(w, r)
// 	})

// 	return server
// }
