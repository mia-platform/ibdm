// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sonarqubeUpstreamDir    = "testdata/upstream/sonarqube"
	sonarqubeWebhookPath    = "/sonarqube/webhook"
	sonarqubeSignatureKey   = "X-Sonar-Webhook-HMAC-SHA256"
	sonarqubeAPIVersion     = "sonarqube.mia-platform.eu/v1"
	sonarqubeIssuesPath     = "/api/issues/search"
	sonarqubeStorefrontKey  = "acme-widgets:storefront"
	sonarqubePartOfTypeRef  = "urn:mia-platform-catalog:mia-platform.eu:v1:RelationshipType:part-of.mia-platform.eu"
	sonarqubeIssuesFamily   = "issues"
	sonarqubeRunsFamily     = "runs"
	sonarqubeRelationsLabel = relationshipsAPIVersion + " relationships"
)

// sonarqubeEnv points the SonarQube source at upstream with SCM links disabled, the webhook at
// the shared secret, and the Catalog destination at catalog.
func sonarqubeEnv(upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
	env := catalog.env()
	env["SONARQUBE_URL"] = upstream.baseURL()
	env["SONARQUBE_TOKEN"] = "test-token"
	env["SONARQUBE_SCM_PROVIDER"] = "none"
	env["SONARQUBE_WEBHOOK_SECRET"] = webhookSecret
	return env
}

// sonarqubeSignature signs body as SonarQube does: bare hex of HMAC-SHA256 with the secret.
func sonarqubeSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// sonarqubeRunField returns the spec field key of every run item.
func sonarqubeRunField(t *testing.T, items []map[string]any, key string) []any {
	t.Helper()
	return specValues(t, itemsOf(items, sonarqubeAPIVersion, sonarqubeRunsFamily), key)
}

// TestSonarQubeSync covers ibdm sync sonarqube, and the dependency claims of the "Internal
// Mappings and Their Dependencies" section of docs/how-to/120_sonarqube-source.md.
func TestSonarQubeSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args   []string
		golden string
		check  func(t *testing.T, items []map[string]any, upstream *fakeUpstream)
	}{
		"SQ1 all internal mappings": {
			args:   []string{"--include-internal-mappings=all"},
			golden: "sonarqube/all",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					sonarqubeAPIVersion + " " + sonarqubeRunsFamily:   2,
					sonarqubeAPIVersion + " " + sonarqubeIssuesFamily: 2,
					sonarqubeRelationsLabel:                           2,
				}, countItems(items), "one run per project, the two storefront issues, one part-of relationship per issue")
				assert.Equal(t, []any{float64(0), float64(2)}, sortedNumbers(t, sonarqubeRunField(t, items, "issuesRead")), "the runs carry their issue counts")
				for _, relationship := range itemsOf(items, relationshipsAPIVersion, "relationships") {
					assert.Equal(t, sonarqubePartOfTypeRef, relationship["data"].(map[string]any)["typeRef"])
				}
				assertRelationshipsResolve(t, items)
				assert.Equal(t, 2, upstream.calls(http.MethodGet, sonarqubeIssuesPath), "one issue search per project")
			},
		},
		"SQ2 runs alone read no issue": {
			args:   []string{"--include-internal-mappings=runs"},
			golden: "sonarqube/runs-only",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{sonarqubeAPIVersion + " " + sonarqubeRunsFamily: 2}, countItems(items))
				assert.Zero(t, upstream.calls(http.MethodGet, sonarqubeIssuesPath), "no issue is read")
				for _, key := range []string{"issuesRead", "truncated", "issueCounts"} {
					assert.Equal(t, []any{nil, nil}, sonarqubeRunField(t, items, key), "%s is null when issues are not selected", key)
				}
			},
		},
		"SQ3 issues alone have no relationship to a run": {
			args:   []string{"--include-internal-mappings=issues"},
			golden: "sonarqube/issues-only",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{sonarqubeAPIVersion + " " + sonarqubeIssuesFamily: 2}, countItems(items),
					"the part-of relationship is not created: the run key is only computed when runs are selected")
				assert.Equal(t, 2, upstream.calls(http.MethodGet, sonarqubeIssuesPath))
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, sonarqubeUpstreamDir))
			catalog := newFakeCatalog(t)

			result := runIBDM(t, sonarqubeEnv(upstream, catalog), append([]string{"sync", "sonarqube"}, test.args...)...)
			require.Equal(t, 0, result.exitCode, "stderr:\n%s", result.stderr)
			assert.Zero(t, countLogsAtLevel(result.logs, levelError), "stderr:\n%s", result.stderr)

			test.check(t, catalog.received(), upstream)
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}

// sortedNumbers returns the numeric values sorted, failing on any other value.
func sortedNumbers(t *testing.T, values []any) []any {
	t.Helper()

	numbers := make([]float64, 0, len(values))
	for _, value := range values {
		number, ok := value.(float64)
		require.True(t, ok, "%v is not a number", value)
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)
	sorted := make([]any, 0, len(numbers))
	for _, number := range numbers {
		sorted = append(sorted, number)
	}
	return sorted
}

// TestSonarQubeWebhook covers ibdm run sonarqube end to end, and the webhook claim of the
// dependency section of docs/how-to/120_sonarqube-source.md.
func TestSonarQubeWebhook(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args          []string
		env           map[string]string
		payload       string
		secret        string
		unsigned      bool
		expectedCode  int
		expectedItems int
		golden        string
		check         func(t *testing.T, items []map[string]any, upstream *fakeUpstream)
	}{
		"SW1 a signed successful analysis records the run and its issues": {
			args:          []string{"--include-internal-mappings=all"},
			payload:       "analysis-success.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1 + 2 + 2,
			golden:        "sonarqube/webhook-analysis-success",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					sonarqubeAPIVersion + " " + sonarqubeRunsFamily:   1,
					sonarqubeAPIVersion + " " + sonarqubeIssuesFamily: 2,
					sonarqubeRelationsLabel:                           2,
				}, countItems(items))
				assertRelationshipsResolve(t, items)
				require.Len(t, upstream.received(), 1, "the issues of the analysed branch are the only read")
				assert.Equal(t, sonarqubeIssuesPath, upstream.received()[0].Path)
				assert.Contains(t, upstream.received()[0].Query, "branch=main")
			},
		},
		"SW2 a failed analysis records only its run": {
			args:          []string{"--include-internal-mappings=all"},
			payload:       "analysis-failed.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "sonarqube/webhook-analysis-failed",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{sonarqubeAPIVersion + " " + sonarqubeRunsFamily: 1}, countItems(items))
				assert.Equal(t, []any{"FAILED"}, sonarqubeRunField(t, items, "status"))
				assert.Empty(t, upstream.received(), "no issue is read for an analysis that did not complete")
			},
		},
		"SW3 issues alone ignore a failed analysis": {
			args:         []string{"--include-internal-mappings=issues"},
			payload:      "analysis-failed.json",
			expectedCode: http.StatusNoContent,
		},
		"SW4 a badly signed delivery is refused": {
			args:         []string{"--include-internal-mappings=all"},
			payload:      "analysis-success.json",
			secret:       "not-the-webhook-secret",
			expectedCode: http.StatusInternalServerError,
		},
		"SW5 an unsigned delivery is refused while a secret is set": {
			args:         []string{"--include-internal-mappings=all"},
			payload:      "analysis-success.json",
			unsigned:     true,
			expectedCode: http.StatusInternalServerError,
		},
		"SW6 an unsigned delivery is accepted when explicitly allowed without a secret": {
			args:          []string{"--include-internal-mappings=runs"},
			env:           map[string]string{"SONARQUBE_WEBHOOK_SECRET": "", "SONARQUBE_WEBHOOK_ALLOW_UNSIGNED": "true"},
			payload:       "analysis-success.json",
			unsigned:      true,
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "sonarqube/webhook-unsigned-runs",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{sonarqubeAPIVersion + " " + sonarqubeRunsFamily: 1}, countItems(items))
				assert.Empty(t, upstream.received())
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, sonarqubeUpstreamDir))
			catalog := newFakeCatalog(t)
			env := sonarqubeEnv(upstream, catalog)
			for key, value := range test.env {
				env[key] = value
			}
			proc := startIBDM(t, env, append([]string{"run", "sonarqube"}, test.args...)...)

			secret := test.secret
			if secret == "" {
				secret = webhookSecret
			}
			body := webhookPayload(t, "sonarqube", test.payload)
			header := http.Header{}
			if !test.unsigned {
				header.Set(sonarqubeSignatureKey, sonarqubeSignature(body, secret))
			}

			require.Equal(t, test.expectedCode, postWebhook(t, proc, sonarqubeWebhookPath, header, body))
			if test.expectedItems == 0 {
				assertNothingArrives(t, catalog)
				assert.Empty(t, upstream.received())
				return
			}

			items := catalog.waitFor(t, test.expectedItems, webhookTimeout)
			require.Len(t, items, test.expectedItems)
			test.check(t, items, upstream)
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}
