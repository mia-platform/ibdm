// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mia-platform/ibdm/internal/source"
)

var (
	testTime = time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
)

func init() {
	timeSource = func() time.Time {
		return testTime
	}
}

func TestSyncSupportedTypes(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		typesToSync  map[string]source.Extra
		expectedData []source.Data
	}{
		"only one type": {
			typesToSync: map[string]source.Extra{
				"gitrepository": {},
			},
			expectedData: []source.Data{
				{
					Type:      "gitrepository",
					Operation: source.DataOperationUpsert,
					Time:      testTime,
					Values: map[string]any{
						"id":   "repo1-id",
						"name": "repo1",
						"url":  "https://dev.azure.com/myorg/_apis/git/repositories/repo1-id",
						"project": map[string]any{
							"id":   "project1-id",
							"name": "project1",
							"url":  "https://dev.azure.com/myorg/_apis/projects/project1-id",
						},
						"remoteUrl": "https://dev.azure.com/myorg/project1/_git/repo1",
					},
				},
				{
					Type:      "gitrepository",
					Operation: source.DataOperationUpsert,
					Time:      testTime,
					Values: map[string]any{
						"id":   "repo2-id",
						"name": "repo2",
						"url":  "https://dev.azure.com/myorg/_apis/git/repositories/repo2-id",
						"project": map[string]any{
							"id":   "project2-id",
							"name": "project2",
							"url":  "https://dev.azure.com/myorg/_apis/projects/project2-id",
						},
						"remoteUrl": "https://dev.azure.com/myorg/project2/_git/repo2",
					},
				},
			},
		},
		"multiple type with also continuation tokens": {
			typesToSync: map[string]source.Extra{
				"team":          {},
				"gitrepository": {},
			},
			expectedData: []source.Data{
				{
					Type:      "gitrepository",
					Operation: source.DataOperationUpsert,
					Time:      testTime,
					Values: map[string]any{
						"id":   "repo1-id",
						"name": "repo1",
						"url":  "https://dev.azure.com/myorg/_apis/git/repositories/repo1-id",
						"project": map[string]any{
							"id":   "project1-id",
							"name": "project1",
							"url":  "https://dev.azure.com/myorg/_apis/projects/project1-id",
						},
						"remoteUrl": "https://dev.azure.com/myorg/project1/_git/repo1",
					},
				},
				{
					Type:      "gitrepository",
					Operation: source.DataOperationUpsert,
					Time:      testTime,
					Values: map[string]any{
						"id":   "repo2-id",
						"name": "repo2",
						"url":  "https://dev.azure.com/myorg/_apis/git/repositories/repo2-id",
						"project": map[string]any{
							"id":   "project2-id",
							"name": "project2",
							"url":  "https://dev.azure.com/myorg/_apis/projects/project2-id",
						},
						"remoteUrl": "https://dev.azure.com/myorg/project2/_git/repo2",
					},
				},
				{
					Type:      "team",
					Operation: source.DataOperationUpsert,
					Time:      testTime,
					Values: map[string]any{
						"id":   "team1-id",
						"name": "team1",
						"url":  "https://dev.azure.com/myorg/_apis/teams/team1-id",
					},
				},
				{
					Type:      "team",
					Operation: source.DataOperationUpsert,
					Time:      testTime,
					Values: map[string]any{
						"id":   "team2-id",
						"name": "team2",
						"url":  "https://dev.azure.com/myorg/_apis/teams/team2-id",
					},
				},
			},
		},
	}

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
				assert.NoError(t, err)
				client := &client{
					organizationURL: serverURL,
				}

				assert.NoError(t, syncResources(ctx, client, test.typesToSync, dataChannel))
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
		if r.Body != nil {
			defer r.Body.Close()
		}

		acceptHeader := r.Header.Get("Accept")
		assert.Contains(t, acceptHeader, "api-version=7.1")
		switch {
		case r.Method == http.MethodGet && r.RequestURI == "/_apis/git/repositories?includeAllUrls=true&includeHidden=true&includeLinks=true":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write(repositoriesRawJSON)
			assert.NoError(t, err)
			return
		case r.Method == http.MethodGet && r.RequestURI == "/_apis/teams?%24expandIdentity=true":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(continuationHeader, "123") //nolint:canonicalheader
			_, err := w.Write(teamsRawJSON1)
			assert.NoError(t, err)
			return
		case r.Method == http.MethodGet && r.RequestURI == "/_apis/teams?%24expandIdentity=true&continuationToken=123":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write(teamsRawJSON2)
			assert.NoError(t, err)
			return
		}

		assert.Fail(t, "unexpected remote call", "request", r.RequestURI, "method", r.Method)
		http.NotFound(w, r)
	})

	return server
}

var (
	repositoriesRawJSON = json.RawMessage(`{
	"count": 2,
	"value": [
		{
			"id": "repo1-id",
			"name": "repo1",
			"url": "https://dev.azure.com/myorg/_apis/git/repositories/repo1-id",
			"project": {
				"id": "project1-id",
				"name": "project1",
				"url": "https://dev.azure.com/myorg/_apis/projects/project1-id"
			},
			"remoteUrl": "https://dev.azure.com/myorg/project1/_git/repo1"
		},
		{
			"id": "repo2-id",
			"name": "repo2",
			"url": "https://dev.azure.com/myorg/_apis/git/repositories/repo2-id",
			"project": {
				"id": "project2-id",
				"name": "project2",
				"url": "https://dev.azure.com/myorg/_apis/projects/project2-id"
			},
			"remoteUrl": "https://dev.azure.com/myorg/project2/_git/repo2"
		}
	]
}`)

	teamsRawJSON1 = json.RawMessage(`{
	"count": 1,
	"value": [
		{
			"id": "team1-id",
			"name": "team1",
			"url": "https://dev.azure.com/myorg/_apis/teams/team1-id"
		}
	]
}`)
	teamsRawJSON2 = json.RawMessage(`{
	"count": 1,
	"value": [
		{
			"id": "team2-id",
			"name": "team2",
			"url": "https://dev.azure.com/myorg/_apis/teams/team2-id"
		}
	]
}`)
)
