// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	bitbucketUpstreamDir = "testdata/upstream/bitbucket"
	bitbucketWebhookPath = "/bitbucket/webhook"

	bitbucketAPIVersion = "bitbucket.mia-platform.eu/v1"

	bitbucketWorkspacesPath      = "/2.0/user/workspaces"
	bitbucketRepositoriesPath    = "/2.0/repositories/acme-widgets"
	bitbucketLabsPath            = "/2.0/repositories/acme-labs"
	bitbucketGizmoPipelinesPath  = "/2.0/repositories/acme-widgets/gizmo-service/pipelines"
	bitbucketGizmoRepositoryPath = "/2.0/repositories/acme-widgets/gizmo-service"

	bitbucketEventHeader     = "X-Event-Key"
	bitbucketSignatureHeader = "X-Hub-Signature"

	bitbucketPipelinesErrorMessage = "error syncing pipelines for repository, skipping"
)

// bitbucketRepositoryNames are the repositories of acme-widgets in the fixtures, over two pages.
var bitbucketRepositoryNames = []string{"acme-widgets/cog-docs", "acme-widgets/gizmo-service", "acme-widgets/sprocket-ui"}

// bitbucketEnv points the Bitbucket source at upstream with a Bearer token and no workspace, so
// that the workspaces are discovered, and the Catalog destination at catalog.
func bitbucketEnv(upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
	env := catalog.env()
	env["BITBUCKET_URL"] = upstream.baseURL()
	env["BITBUCKET_ACCESS_TOKEN"] = "test-token"
	env["BITBUCKET_WEBHOOK_SECRET"] = webhookSecret
	return env
}

// bitbucketSignature signs body as Bitbucket does: HMAC-SHA256 with the secret, hex encoded,
// with the sha256= prefix. It is the scheme GitHub uses too.
func bitbucketSignature(body []byte, secret string) string {
	return githubSignature(body, secret)
}

// TestBitbucketSync covers ibdm sync bitbucket, and checks the dependency claims of
// docs/how-to/110_bitbucket-source.md.
func TestBitbucketSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args             []string
		env              map[string]string
		expectedPipeline int
		golden           string
		check            func(t *testing.T, items []map[string]any, upstream *fakeUpstream)
	}{
		"B1 all internal mappings, with workspace discovery and absolute next URLs": {
			args:             []string{"--include-internal-mappings=all"},
			expectedPipeline: 1,
			golden:           "bitbucket/all",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					bitbucketAPIVersion + " repositories":      3,
					bitbucketAPIVersion + " pipelines":         3,
					relationshipsAPIVersion + " relationships": 3,
				}, countItems(items), "gizmo-service holds 3 pipelines over two pages; sprocket-ui fails; cog-docs has none")
				assert.Equal(t, bitbucketRepositoryNames, sortedSpecValues(t, itemsOf(items, bitbucketAPIVersion, "repositories"), "fullName"),
					"cog-docs comes from page 2, reached through the absolute next URL")
				assert.Equal(t, 1, upstream.calls(http.MethodGet, bitbucketWorkspacesPath), "BITBUCKET_WORKSPACE unset: the workspaces are discovered")
				assert.Equal(t, 1, upstream.calls(http.MethodGet, bitbucketLabsPath), "every discovered workspace with a slug is walked")
				assert.Equal(t, 2, upstream.calls(http.MethodGet, bitbucketRepositoriesPath))
				assert.Equal(t, 2, upstream.calls(http.MethodGet, bitbucketGizmoPipelinesPath))
				assertRelationshipsResolve(t, items)
			},
		},
		"B2 repositories alone fetch no pipeline": {
			args:   []string{"--include-internal-mappings=repositories"},
			golden: "bitbucket/repositories-only",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{bitbucketAPIVersion + " repositories": 3}, countItems(items))
				assert.Zero(t, upstream.calls(http.MethodGet, bitbucketGizmoPipelinesPath))
			},
		},
		"B3 pipelines alone list the repositories without writing them": {
			args:             []string{"--include-internal-mappings=pipelines"},
			expectedPipeline: 1,
			golden:           "bitbucket/pipelines-only",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					bitbucketAPIVersion + " pipelines":         3,
					relationshipsAPIVersion + " relationships": 3,
				}, countItems(items))
				assert.Equal(t, 2, upstream.calls(http.MethodGet, bitbucketRepositoriesPath), "the repositories are still listed")
				for _, relationship := range itemsOf(items, relationshipsAPIVersion, "relationships") {
					spec, ok := relationship["data"].(map[string]any)
					require.True(t, ok)
					assert.Contains(t, spec["targetRef"], "urn:mia-platform-catalog:bitbucket.mia-platform.eu:v1:Repository:",
						"the relationships point at repository items nothing creates")
				}
			},
		},
		"B4 a configured workspace with basic auth skips the discovery": {
			args: []string{"--include-internal-mappings=repositories"},
			env: map[string]string{
				"BITBUCKET_WORKSPACE":    "acme-widgets",
				"BITBUCKET_ACCESS_TOKEN": "",
				"BITBUCKET_API_USERNAME": "jane-doe",
				"BITBUCKET_API_TOKEN":    "test-token",
			},
			golden: "bitbucket/configured-workspace",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{bitbucketAPIVersion + " repositories": 3}, countItems(items))
				assert.Zero(t, upstream.calls(http.MethodGet, bitbucketWorkspacesPath))
				assert.Zero(t, upstream.calls(http.MethodGet, bitbucketLabsPath))
				for _, request := range upstream.received() {
					assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("jane-doe:test-token")), request.Header["Authorization"], "basic auth with jane-doe:test-token")
				}
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, bitbucketUpstreamDir), "Authorization")
			catalog := newFakeCatalog(t)
			env := bitbucketEnv(upstream, catalog)
			for key, value := range test.env {
				if value == "" {
					delete(env, key)
					continue
				}
				env[key] = value
			}

			result := runIBDM(t, env, append([]string{"sync", "bitbucket"}, test.args...)...)
			require.Equal(t, 0, result.exitCode, "stderr:\n%s", result.stderr)
			assert.Equal(t, test.expectedPipeline, countLogs(result.logs, levelError, bitbucketPipelinesErrorMessage),
				"the failing pipelines endpoint of sprocket-ui is logged and skipped")
			assert.Equal(t, test.expectedPipeline, countLogsAtLevel(result.logs, levelError), "no other error; stderr:\n%s", result.stderr)

			test.check(t, catalog.received(), upstream)
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}

// TestBitbucketWebhook covers ibdm run bitbucket end to end.
func TestBitbucketWebhook(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args          []string
		eventKey      string
		payload       string
		secret        string
		expectedCode  int
		expectedItems int
		golden        string
		check         func(t *testing.T, items []map[string]any, upstream *fakeUpstream)
	}{
		"BW1 a signed repo:push event is written, enriched from the API": {
			args:          []string{"--include-internal-mappings=all"},
			eventKey:      "repo:push",
			payload:       "repo-push-gizmo-service.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "bitbucket/webhook-repo-push",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, 1, upstream.calls(http.MethodGet, bitbucketGizmoRepositoryPath))
				assert.Equal(t, []any{"Gizmo service (as read by the enrichment call)"}, specValues(t, items, "description"),
					"the item is rendered from the repository the API returned, not from the payload")
			},
		},
		"BW2 a badly signed event is refused": {
			args:         []string{"--include-internal-mappings=all"},
			eventKey:     "repo:push",
			payload:      "repo-push-gizmo-service.json",
			secret:       "not-the-webhook-secret",
			expectedCode: http.StatusInternalServerError,
		},
		"BW3 a failed enrichment falls back to the payload": {
			args:          []string{"--include-internal-mappings=repositories"},
			eventKey:      "repo:updated",
			payload:       "repo-updated-legacy-tool.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "bitbucket/webhook-enrichment-fallback",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, 1, upstream.calls(http.MethodGet, "/2.0/repositories/acme-widgets/legacy-tool"))
				assert.Equal(t, []any{"Legacy tool (as sent in the webhook payload)"}, specValues(t, items, "description"))
			},
		},
		"BW4 pipelines are never produced by the webhook": {
			args:         []string{"--include-internal-mappings=pipelines"},
			eventKey:     "repo:push",
			payload:      "repo-push-gizmo-service.json",
			expectedCode: http.StatusNoContent,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, bitbucketUpstreamDir))
			catalog := newFakeCatalog(t)
			proc := startIBDM(t, bitbucketEnv(upstream, catalog), append([]string{"run", "bitbucket"}, test.args...)...)

			secret := test.secret
			if secret == "" {
				secret = webhookSecret
			}
			body := webhookPayload(t, "bitbucket", test.payload)
			header := http.Header{bitbucketEventHeader: {test.eventKey}, bitbucketSignatureHeader: {bitbucketSignature(body, secret)}}

			require.Equal(t, test.expectedCode, postWebhook(t, proc, bitbucketWebhookPath, header, body))
			if test.expectedItems == 0 {
				assertNothingArrives(t, catalog)
				assert.Empty(t, upstream.received(), "nothing is fetched")
				return
			}

			items := catalog.waitFor(t, test.expectedItems, webhookTimeout)
			require.Len(t, items, test.expectedItems)
			test.check(t, items, upstream)
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}
