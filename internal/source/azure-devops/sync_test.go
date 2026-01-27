// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func init() {
	timeSource = func() time.Time {
		return time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	}
}

func TestSyncSupportedTypes(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		typesToSync   map[string]source.Extra
		expectedData  []source.Data
		expectedError error
	}{}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			server := testServer(t)
			defer server.Close()

			dataChannel := make(chan source.Data)
			go func(ctx context.Context) {
				defer close(dataChannel)

				serverURL, err := url.Parse(server.URL)
				require.NoError(t, err)
				client := &client{
					organizationURL: serverURL,
				}

				err = syncResources(ctx, client, test.typesToSync, dataChannel)
				if test.expectedError != nil {
					assert.ErrorIs(t, err, test.expectedError)
					return
				}
				assert.NoError(t, err)
			}(ctx)

			var actualData []source.Data
		loop:
			for {
				select {
				case <-ctx.Done():
					assert.Fail(t, "test timed out")
					break loop
				case data, open := <-dataChannel:
					if !open {
						break loop
					}
					actualData = append(actualData, data)
				}
			}

			assert.ElementsMatch(t, test.expectedData, actualData)
		})
	}
}

func testServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(nil)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.RequestURI == "/_apis/repositories?includeAllUrls=true&includeHidden=true&includeLinks=true":
			w.Header().Set("Content-Type", "application/json")
			return
		}

		assert.Fail(t, "unexpected remote call", "request", r.RequestURI, "method", r.Method)
		http.NotFound(w, r)
	})

	return server
}
