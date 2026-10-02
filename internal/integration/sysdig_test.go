// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sysdigUpstreamDir        = "testdata/upstream/sysdig"
	sysdigFailingUpstreamDir = "testdata/upstream/sysdig/failing"
	sysdigPagedUpstreamDir   = "testdata/upstream/sysdig/paged"
	sysdigMappingsDir        = "testdata/mappings/sysdig"
	sysdigWebhookPath        = "/sysdig/webhook"

	sysdigAPIVersion  = "sysdig.mia-platform.eu/v1"
	sysdigSysQLPath   = "/api/sysql/v2/query"
	sysdigResultsPath = "/secure/vulnerability/v1beta1/results/"

	// sysdigNexusGroup is written by the Nexus integration: vulnerabilities point at its Docker images.
	sysdigNexusGroup = "nexus.mia-platform.eu"
)

// sysdigEnv points both Sysdig APIs at upstream, and the Catalog destination at catalog.
func sysdigEnv(upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
	env := catalog.env()
	env["SYSDIG_URL"] = upstream.baseURL()
	env["SYSDIG_API_TOKEN"] = "test-token"
	env["SYSDIG_BASE_URL"] = upstream.baseURL()
	env["SYSDIG_BEARER_TOKEN"] = "test-bearer-token"
	return env
}

// TestSysdigSync covers ibdm sync sysdig. SysQL pages are told apart by the OFFSET in the query
// body, which the fake upstream matches.
func TestSysdigSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		upstreamDir string
		pageSize    string
		golden      string
		check       func(t *testing.T, result result, upstream *fakeUpstream, items []map[string]any)
	}{
		"every vulnerability over two pages, until the empty page": {
			upstreamDir: sysdigPagedUpstreamDir,
			pageSize:    "2",
			golden:      "sysdig/sync-all",
			check: func(t *testing.T, result result, upstream *fakeUpstream, items []map[string]any) {
				t.Helper()
				assert.Equal(t, 3, upstream.calls(http.MethodPost, sysdigSysQLPath), "OFFSET 0, 2 and 3: the empty page ends the pagination")
				assert.Zero(t, countLogsAtLevel(result.logs, levelError), "stderr:\n%s", result.stderr)
				assert.Equal(t, map[string]int{
					sysdigAPIVersion + " vulnerabilities":      3,
					relationshipsAPIVersion + " relationships": 3,
				}, countItems(items), "one vulnerability and one relationship per SysQL item, from both pages")
				assertRelationshipsResolve(t, items, sysdigNexusGroup)
			},
		},
		"an empty first page ends the sync": {
			upstreamDir: sysdigUpstreamDir,
			golden:      "sysdig/sync-empty",
			check: func(t *testing.T, result result, upstream *fakeUpstream, items []map[string]any) {
				t.Helper()
				assert.Empty(t, items)
				assert.Equal(t, 1, upstream.calls(http.MethodPost, sysdigSysQLPath), "fetched_items_count 0 stops the pagination")
				assert.Zero(t, countLogsAtLevel(result.logs, levelError), "stderr:\n%s", result.stderr)
			},
		},
		"a failing query is logged and the sync still succeeds documentsCurrentBehaviour": {
			upstreamDir: sysdigFailingUpstreamDir,
			golden:      "sysdig/sync-failing",
			check: func(t *testing.T, result result, upstream *fakeUpstream, items []map[string]any) {
				t.Helper()
				assert.Empty(t, items)
				assert.Equal(t, 1, upstream.calls(http.MethodPost, sysdigSysQLPath))
				assert.Equal(t, 1, countLogs(result.logs, levelError, "error syncing data type"), "per-type errors are logged and swallowed")
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, test.upstreamDir), "Authorization")
			catalog := newFakeCatalog(t)

			env := sysdigEnv(upstream, catalog)
			if test.pageSize != "" {
				env["SYSDIG_PAGE_SIZE"] = test.pageSize
			}
			result := runIBDM(t, env, "sync", "sysdig", "--include-internal-mappings=all")
			require.Equal(t, 0, result.exitCode, "stderr:\n%s", result.stderr)

			test.check(t, result, upstream, catalog.received())
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}

// TestSysdigWebhook covers ibdm run sysdig end to end. Sysdig notifications carry no signature,
// so there is no rejected-authentication case: an unsigned notification is accepted.
func TestSysdigWebhook(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args          []string
		payload       string
		expectedItems int
		golden        string
		check         func(t *testing.T, items []map[string]any, upstream *fakeUpstream)
	}{
		"a failed Docker image scan writes one item per vulnerability, documentsCurrentBehaviour": {
			args:          []string{"--include-internal-mappings=all"},
			payload:       "pipeline-failure-failed.json",
			expectedItems: 3 + 3,
			golden:        "sysdig/webhook-failed-image",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					sysdigAPIVersion + " vulnerabilities":      3,
					relationshipsAPIVersion + " relationships": 3,
				}, countItems(items), "3 vulnerabilities over 2 packages, each related to the Docker image")
				assert.Equal(t, []string{"CVE-2026-0001", "CVE-2026-0002", "CVE-2026-0003"},
					sortedSpecValues(t, itemsOf(items, sysdigAPIVersion, "vulnerabilities"), "name"))
				assertRelationshipsResolve(t, items, sysdigNexusGroup)
				assert.Equal(t, 1, upstream.calls(http.MethodGet, sysdigResultsPath+"res-failed-0001"))
				assert.Zero(t, upstream.calls(http.MethodPost, sysdigSysQLPath), "the webhook does not query SysQL")

				// Finding F-S1: the webhook emits exploitable and no package data, while the mapping
				// reads hasExploit, packageName and packageVersion, so they stay empty.
				critical := itemsOf(items, sysdigAPIVersion, "vulnerabilities")
				for _, item := range critical {
					spec, _ := item["data"].(map[string]any)
					if spec["name"] == "CVE-2026-0001" {
						assert.Empty(t, spec["hasExploit"], "the source sent exploitable: true")
						assert.Empty(t, spec["packageName"], "the source result names the package openssl")
					}
				}
			},
		},
		"an external mapping on the vulnerability type fans out": {
			args:          []string{"--include-internal-mappings=all", "-f", filepath.Join(sysdigMappingsDir, "fanout")},
			payload:       "pipeline-failure-failed.json",
			expectedItems: 3 + 3 + 3,
			golden:        "sysdig/webhook-fanout",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, []string{"critical", "high", "medium"},
					sortedSpecValues(t, itemsOf(items, externalAPIVersion, "vulnerabilityseverities"), "severity"))
				assert.Equal(t, 1, upstream.calls(http.MethodGet, sysdigResultsPath+"res-failed-0001"), "both mappings share one fetch")
			},
		},
		"a passed scan writes nothing": {
			args:    []string{"--include-internal-mappings=all"},
			payload: "pipeline-failure-passed.json",
			check: func(t *testing.T, _ []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, 1, upstream.calls(http.MethodGet, sysdigResultsPath+"res-passed-0002"), "the result is read, then dropped")
			},
		},
		"a failed scan that is not a Docker image writes nothing": {
			args:    []string{"--include-internal-mappings=all"},
			payload: "pipeline-failure-host.json",
			check: func(t *testing.T, _ []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, 1, upstream.calls(http.MethodGet, sysdigResultsPath+"res-host-0003"))
			},
		},
		"an unknown event is ignored without any fetch": {
			args:    []string{"--include-internal-mappings=all"},
			payload: "unknown-event.json",
			check: func(t *testing.T, _ []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Empty(t, upstream.received())
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, sysdigUpstreamDir), "Authorization")
			catalog := newFakeCatalog(t)
			proc := startIBDM(t, sysdigEnv(upstream, catalog), append([]string{"run", "sysdig"}, test.args...)...)

			// No signature header: Sysdig notifications are not signed.
			body := webhookPayload(t, "sysdig", test.payload)
			require.Equal(t, http.StatusNoContent, postWebhook(t, proc, sysdigWebhookPath, http.Header{}, body))

			if test.expectedItems == 0 {
				assertNothingArrives(t, catalog)
				test.check(t, nil, upstream)
				return
			}

			items := catalog.waitFor(t, test.expectedItems, webhookTimeout)
			require.Len(t, items, test.expectedItems)
			test.check(t, items, upstream)
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}
