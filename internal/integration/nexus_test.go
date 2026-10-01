// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	nexusUpstreamDir = "testdata/upstream/nexus"
	nexusMappingsDir = "testdata/mappings/nexus"
	nexusWebhookPath = "/nexus/webhook"

	nexusSignatureKey = "X-Nexus-Webhook-Signature"
	nexusEventKey     = "X-Nexus-Webhook-Id"
	nexusEventName    = "rm:repository:component"

	nexusAPIVersion = "nexus.mia-platform.eu/v1"

	nexusRepositoriesPath = "/service/rest/v1/repositories"
	nexusDockerHostedPath = "/service/rest/v1/repositories/docker-hosted"
	nexusComponentsPath   = "/service/rest/v1/components"
	nexusCatalogAPIPath   = "/service/rest/v1/components/component-catalog-api"
)

// nexusImages are the name and version of every docker component of the fixtures, with assets
// or not: their identifiers hash the address of the fake, so the golden files store placeholders.
var nexusImages = [][2]string{
	{"acme-widgets/catalog-api", "1.4.0"},
	{"acme-widgets/checkout-api", "2.0.1"},
	{"acme-widgets/orders-worker", "0.9.0"},
}

// nexusEnv points the Nexus source at upstream and the Catalog destination at catalog. A secret
// enables the webhook signature check.
func nexusEnv(t *testing.T, upstream *fakeUpstream, catalog *fakeCatalog, secret string) map[string]string {
	t.Helper()

	env := catalog.env()
	env["NEXUS_URL_SCHEMA"] = "http"
	env["NEXUS_URL_HOST"] = hostOf(t, upstream.baseURL())
	env["NEXUS_TOKEN_NAME"] = "test-token-name"
	env["NEXUS_TOKEN_PASSCODE"] = "test-token-passcode"
	if secret != "" {
		env["NEXUS_WEBHOOK_SECRET"] = secret
	}
	return env
}

// nexusIdentifier computes the identifier the dockerimages mapping renders: the SHA-256 of
// host/name:version.
func nexusIdentifier(host, name, version string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%s:%s", host, name, version)))
	return hex.EncodeToString(sum[:])
}

// nexusIdentifierReplacements returns the old, new pairs that turn the identifiers of the run
// into stable placeholders.
func nexusIdentifierReplacements(t *testing.T, upstream *fakeUpstream) []string {
	t.Helper()

	host := hostOf(t, upstream.baseURL())
	replacements := make([]string, 0, 2*len(nexusImages))
	for _, image := range nexusImages {
		replacements = append(replacements, nexusIdentifier(host, image[0], image[1]), "<nexus-id "+image[0]+":"+image[1]+">")
	}
	return replacements
}

// nexusSignature signs body as Nexus does: HMAC-SHA1 with the secret, raw hex, no prefix.
func nexusSignature(body []byte, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// nexusItemNames returns the sorted item names of the dockerimages items.
func nexusItemNames(t *testing.T, items []map[string]any) []string {
	t.Helper()

	images := itemsOf(items, nexusAPIVersion, "dockerimages")
	names := make([]string, 0, len(images))
	for _, item := range images {
		metadata, ok := item["metadata"].(map[string]any)
		require.True(t, ok, "item without metadata: %v", item)
		title, ok := metadata["title"].(string)
		require.True(t, ok)
		names = append(names, title)
	}
	return names
}

// TestNexusSync covers ibdm sync nexus end to end.
func TestNexusSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args   []string
		env    map[string]string
		golden string
		check  func(t *testing.T, items []map[string]any, upstream *fakeUpstream)
	}{
		"N1 all internal mappings, docker components with assets only": {
			args:   []string{"--include-internal-mappings=all"},
			golden: "nexus/all",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				host := hostOf(t, upstream.baseURL())
				assert.ElementsMatch(t, []string{
					host + "/acme-widgets/catalog-api:1.4.0",
					host + "/acme-widgets/checkout-api:2.0.1",
				}, nexusItemNames(t, items), "orders-worker has no asset and widget-core is a maven2 component: neither is emitted")
				for _, item := range items {
					spec, ok := item["data"].(map[string]any)
					require.True(t, ok)
					assert.Equal(t, nexusIdentifier(host, spec["name"].(string), spec["version"].(string)), item["name"],
						"the identifier hashes the NEXUS_URL_HOST the source runs with")
				}
				assert.Equal(t, 2, nexusCountQuery(t, upstream, nexusComponentsPath, "repository", "docker-hosted"), "both pages, linked by the continuationToken")
				assert.Equal(t, 1, nexusCountQuery(t, upstream, nexusComponentsPath, "repository", "maven-releases"), "non-docker repositories are still read")
				assertRelationshipsResolve(t, items)
			},
		},
		"N2 a specific repository narrows the walk": {
			args:   []string{"--include-internal-mappings=all"},
			env:    map[string]string{"NEXUS_SPECIFIC_REPOSITORY": "docker-hosted"},
			golden: "nexus/specific-repository",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Len(t, items, 2)
				assert.Equal(t, 1, upstream.calls(http.MethodGet, nexusDockerHostedPath))
				assert.Zero(t, upstream.calls(http.MethodGet, nexusRepositoriesPath), "the repositories are not listed")
				assert.Zero(t, nexusCountQuery(t, upstream, nexusComponentsPath, "repository", "maven-releases"))
			},
		},
		"N3 an external mapping on the dockerimage type fans out": {
			args:   []string{"--include-internal-mappings=all", "-f", filepath.Join(nexusMappingsDir, "fanout")},
			golden: "nexus/fanout",
			check: func(t *testing.T, items []map[string]any, _ *fakeUpstream) {
				t.Helper()
				assert.Len(t, itemsOf(items, nexusAPIVersion, "dockerimages"), 2)
				assert.Equal(t, []string{"acme-widgets/catalog-api:1.4.0", "acme-widgets/checkout-api:2.0.1"},
					sortedSpecValues(t, itemsOf(items, externalAPIVersion, "imagetags"), "image"))
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, nexusUpstreamDir))
			catalog := newFakeCatalog(t)
			env := nexusEnv(t, upstream, catalog, "")
			for key, value := range test.env {
				env[key] = value
			}

			result := runIBDM(t, env, append([]string{"sync", "nexus"}, test.args...)...)
			require.Equal(t, 0, result.exitCode, "stderr:\n%s", result.stderr)
			nexusAssertNoErrorLogs(t, result)

			test.check(t, catalog.received(), upstream)
			assertGolden(t, test.golden, catalog, upstream, nexusIdentifierReplacements(t, upstream)...)
		})
	}
}

// nexusAssertNoErrorLogs fails the test if the run logged an error.
func nexusAssertNoErrorLogs(t *testing.T, run result) {
	t.Helper()

	for _, record := range run.logs {
		assert.NotEqual(t, levelError, record[logLevelKey], "unexpected error log: %v", record)
	}
}

// nexusCountQuery counts the requests to path whose query carries the parameter key with value.
func nexusCountQuery(t *testing.T, upstream *fakeUpstream, path, key, value string) int {
	t.Helper()

	count := 0
	for _, request := range upstream.received() {
		query, err := url.ParseQuery(request.Query)
		require.NoError(t, err)
		if request.Path == path && query.Get(key) == value {
			count++
		}
	}
	return count
}

// TestNexusWebhook covers ibdm run nexus end to end, with and without a webhook secret.
func TestNexusWebhook(t *testing.T) {
	t.Parallel()

	const nexusWrongSecret = "not-the-webhook-secret"

	testCases := map[string]struct {
		secret        string
		payload       string
		signWith      string
		unsigned      bool
		expectedCode  int
		expectedItems int
		golden        string
		check         func(t *testing.T, items []map[string]any, upstream *fakeUpstream, proc *process)
	}{
		"NW1 a signed CREATED event fetches and writes the component": {
			secret:        webhookSecret,
			payload:       "component-created.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "nexus/webhook-component-created",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream, _ *process) {
				t.Helper()
				assert.Equal(t, "upsert", items[0]["operation"])
				assert.Equal(t, 1, upstream.calls(http.MethodGet, nexusCatalogAPIPath))
			},
		},
		"NW2 a signed DELETED event deletes without any fetch": {
			secret:        webhookSecret,
			payload:       "component-deleted.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "nexus/webhook-component-deleted",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream, _ *process) {
				t.Helper()
				assert.Equal(t, "delete", items[0]["operation"])
				assert.Equal(t, nexusIdentifier(hostOf(t, upstream.baseURL()), "acme-widgets/checkout-api", "2.0.1"), items[0]["name"],
					"the delete targets the identifier the sync wrote")
				assert.Empty(t, upstream.received())
			},
		},
		"NW3 a badly signed event is refused": {
			secret:       webhookSecret,
			payload:      "component-created.json",
			signWith:     nexusWrongSecret,
			expectedCode: http.StatusInternalServerError,
		},
		"NW4 an unsigned event is refused when a secret is set": {
			secret:       webhookSecret,
			payload:      "component-created.json",
			unsigned:     true,
			expectedCode: http.StatusInternalServerError,
		},
		"NW5 without a secret an unsigned event is accepted": {
			payload:       "component-created.json",
			unsigned:      true,
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "nexus/webhook-component-created",
			check: func(t *testing.T, items []map[string]any, _ *fakeUpstream, _ *process) {
				t.Helper()
				assert.Equal(t, "upsert", items[0]["operation"])
			},
		},
		"NW6 without a secret a signed event is refused, documentsCurrentBehaviour": {
			payload:      "component-created.json",
			signWith:     nexusWrongSecret,
			expectedCode: http.StatusInternalServerError,
		},
		"NW7 a non-docker component event is ignored": {
			secret:       webhookSecret,
			payload:      "component-created-maven.json",
			expectedCode: http.StatusNoContent,
			check: func(t *testing.T, _ []map[string]any, upstream *fakeUpstream, _ *process) {
				t.Helper()
				assert.Empty(t, upstream.received(), "the component is not fetched")
			},
		},
		"NW8 an event without a timestamp is skipped": {
			secret:       webhookSecret,
			payload:      "component-created-no-timestamp.json",
			expectedCode: http.StatusNoContent,
			check: func(t *testing.T, _ []map[string]any, _ *fakeUpstream, proc *process) {
				t.Helper()
				require.Eventually(t, func() bool {
					return countLogs(proc.logs(), levelError, "error processing webhook event") == 1
				}, webhookTimeout, pollInterval)
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, nexusUpstreamDir))
			catalog := newFakeCatalog(t)
			proc := startIBDM(t, nexusEnv(t, upstream, catalog, test.secret), "run", "nexus", "--include-internal-mappings=all")

			body := webhookPayload(t, "nexus", test.payload)
			header := http.Header{nexusEventKey: {nexusEventName}}
			if !test.unsigned {
				signWith := test.signWith
				if signWith == "" {
					signWith = test.secret
				}
				header.Set(nexusSignatureKey, nexusSignature(body, signWith))
			}

			require.Equal(t, test.expectedCode, postWebhook(t, proc, nexusWebhookPath, header, body))
			if test.expectedItems == 0 {
				assertNothingArrives(t, catalog)
				if test.check != nil {
					test.check(t, nil, upstream, proc)
				}
				return
			}

			items := catalog.waitFor(t, test.expectedItems, webhookTimeout)
			require.Len(t, items, test.expectedItems)
			test.check(t, items, upstream, proc)
			assertGolden(t, test.golden, catalog, upstream, nexusIdentifierReplacements(t, upstream)...)
		})
	}
}
