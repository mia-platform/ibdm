// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	githubWebhookPath  = "/github/webhook"
	githubEventHeader  = "X-GitHub-Event"
	githubSignatureKey = "X-Hub-Signature-256"
)

// TestGitHubWebhook covers ibdm run github end to end: a signed event reaches the Catalog, a
// badly signed one is refused, and events of a type no selected mapping handles are ignored.
func TestGitHubWebhook(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args          []string
		event         string
		payload       string
		secret        string
		expectedCode  int
		expectedItems int
		golden        string
		check         func(t *testing.T, items []map[string]any, upstream *fakeUpstream)
	}{
		"W1 a signed repository event is written": {
			args:          []string{"--include-internal-mappings=all"},
			event:         "repository",
			payload:       "repository-created.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "github/webhook-repository-created",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, "upsert", items[0]["operation"])
				assert.Equal(t, 1, upstream.calls(http.MethodGet, githubGizmoLangsPath), "the event is enriched with the languages")
			},
		},
		"W2 a repository deleted event deletes the item": {
			args:          []string{"--include-internal-mappings=all"},
			event:         "repository",
			payload:       "repository-deleted.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "github/webhook-repository-deleted",
			check: func(t *testing.T, items []map[string]any, _ *fakeUpstream) {
				t.Helper()
				assert.Equal(t, "delete", items[0]["operation"])
				assert.NotContains(t, items[0], "data", "a delete carries no spec")
			},
		},
		"W3 a badly signed event is refused": {
			args:         []string{"--include-internal-mappings=all"},
			event:        "repository",
			payload:      "repository-created.json",
			secret:       "not-the-webhook-secret",
			expectedCode: http.StatusInternalServerError,
		},
		"W4 an event of a type no selected mapping handles is ignored": {
			args:         []string{"--include-internal-mappings=repositories"},
			event:        "workflow_run",
			payload:      "workflow-run-completed.json",
			expectedCode: http.StatusNoContent,
		},
		"W5 an external mapping on an internal type fans out": {
			args:          []string{"--include-internal-mappings=repositories", "-f", githubMapping("fanout")},
			event:         "repository",
			payload:       "repository-created.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 2,
			golden:        "github/webhook-fanout",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Len(t, itemsOf(items, githubAPIVersion, "repositories"), 1)
				assert.Len(t, itemsOf(items, externalAPIVersion, "repositorycards"), 1)
				assert.Equal(t, 1, upstream.calls(http.MethodGet, githubGizmoLangsPath), "both mappings share one fetch: same extra")
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, githubUpstreamDir), githubAPIVersionHeader)
			catalog := newFakeCatalog(t)
			proc := startIBDM(t, githubEnv(upstream, catalog), append([]string{"run", "github"}, test.args...)...)

			secret := test.secret
			if secret == "" {
				secret = webhookSecret
			}
			body := webhookPayload(t, "github", test.payload)
			header := http.Header{githubEventHeader: {test.event}, githubSignatureKey: {githubSignature(body, secret)}}

			require.Equal(t, test.expectedCode, postWebhook(t, proc, githubWebhookPath, header, body))
			if test.expectedItems == 0 {
				assertNothingArrives(t, catalog)
				return
			}

			items := catalog.waitFor(t, test.expectedItems, webhookTimeout)
			require.Len(t, items, test.expectedItems)
			test.check(t, items, upstream)
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}
