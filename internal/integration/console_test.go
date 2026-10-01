// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	consoleUpstreamDir  = "testdata/upstream/console"
	consoleAPIPrefix    = "/api"
	consoleWebhookPath  = "/console/webhook"
	consoleSignatureKey = "X-Mia-Signature"

	consoleAPIVersion = "console.mia-platform.eu/v1"

	consoleProjectsPath  = "/api/backend/projects/"
	consoleCompaniesPath = "/api/user/companies"

	storefrontID = "64f1a0c0e1b2c3d4e5f60001"

	projectURNPrefix  = "urn:mia-platform-catalog:console.mia-platform.eu:v1:Project:"
	revisionURNPrefix = "urn:mia-platform-catalog:console.mia-platform.eu:v1:Revision:"
	clusterURNPrefix  = "urn:mia-platform-catalog:console.mia-platform.eu:v1:Cluster:"

	// nexusURNGroup is written by the Nexus integration: the services point at its Docker images.
	nexusURNGroup = "nexus.mia-platform.eu"
)

// consoleEnv points the Console source at upstream, without authentication, and the Catalog
// destination at catalog.
func consoleEnv(upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
	env := catalog.env()
	env["CONSOLE_ENDPOINT"] = upstream.baseURL() + consoleAPIPrefix
	env["CONSOLE_WEBHOOK_SECRET"] = webhookSecret
	return env
}

// countItems counts the items of every item type, keyed "apiVersion itemFamily".
func countItems(items []map[string]any) map[string]int {
	counts := make(map[string]int)
	for _, item := range items {
		counts[item["apiVersion"].(string)+" "+item["itemFamily"].(string)]++
	}
	return counts
}

// relationshipTargets counts the relationship targetRefs by URN prefix.
func relationshipTargets(t *testing.T, items []map[string]any) map[string]int {
	t.Helper()

	counts := make(map[string]int)
	for _, item := range itemsOf(items, relationshipsAPIVersion, "relationships") {
		spec, ok := item["data"].(map[string]any)
		require.True(t, ok)
		target, ok := spec["targetRef"].(string)
		require.True(t, ok)
		for _, prefix := range []string{projectURNPrefix, revisionURNPrefix, clusterURNPrefix} {
			if strings.HasPrefix(target, prefix) {
				counts[prefix]++
			}
		}
	}
	return counts
}

// projectWalkCalls counts the requests of the project walk: projects, revisions, configurations.
func projectWalkCalls(upstream *fakeUpstream) int {
	count := 0
	for _, request := range upstream.received() {
		if strings.HasPrefix(request.Path, consoleProjectsPath) {
			count++
		}
	}
	return count
}

// TestConsoleSync covers ibdm sync console, and checks the dependency claims of the "Internal
// Mappings and Their Dependencies" section of docs/how-to/050_console-source.md.
func TestConsoleSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args   []string
		golden string
		check  func(t *testing.T, items []map[string]any, upstream *fakeUpstream)
	}{
		"C1 all internal mappings": {
			args:   []string{"--include-internal-mappings=all"},
			golden: "console/all",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					consoleAPIVersion + " projects":            2,
					consoleAPIVersion + " revisions":           3,
					consoleAPIVersion + " services":            1,
					consoleAPIVersion + " customresources":     1,
					consoleAPIVersion + " clusters":            1,
					relationshipsAPIVersion + " relationships": 3 + 3 + 2 + 2,
				}, countItems(items), "relationships: 3 revision→project, 3 from the service, 2 from the custom resource, 2 cluster↔project")
				assert.Equal(t, []string{"catalog-api"}, serviceNames(t, items),
					"only the custom, non-advanced service of the default branch: not legacy-gateway (advanced), api-gateway (core) or checkout-api (feature branch)")
				assert.Equal(t, 1, upstream.calls(http.MethodGet, consoleCompaniesPath))
				assertRelationshipsResolve(t, items, nexusURNGroup)
			},
		},
		"C2 projects alone still walk revisions and configurations": {
			args:   []string{"--include-internal-mappings=projects"},
			golden: "console/projects-only",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{consoleAPIVersion + " projects": 2}, countItems(items))
				assert.Equal(t, 1+2+3, projectWalkCalls(upstream), "the project walk reads the revisions and configurations even when only projects are written")
				assert.Zero(t, upstream.calls(http.MethodGet, consoleCompaniesPath), "the cluster walk does not run")
			},
		},
		"C3 excluding projects leaves relationships towards missing project items": {
			args:   []string{"--include-internal-mappings=all", "--exclude-internal-mappings=projects"},
			golden: "console/all-but-projects",
			check: func(t *testing.T, items []map[string]any, _ *fakeUpstream) {
				t.Helper()
				assert.Empty(t, itemsOf(items, consoleAPIVersion, "projects"))
				assert.Equal(t, 3+1+1, relationshipTargets(t, items)[projectURNPrefix],
					"revisions, the service and the custom resource still point at project items nothing creates")
			},
		},
		"C4 the cluster walk alone": {
			args:   []string{"--include-internal-mappings=clusters,cluster-project-relationships"},
			golden: "console/clusters",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					consoleAPIVersion + " clusters":            1,
					relationshipsAPIVersion + " relationships": 2,
				}, countItems(items))
				assert.Zero(t, projectWalkCalls(upstream), "the project walk does not run")
			},
		},
		"C5 services alone walk projects and revisions without writing them": {
			args:   []string{"--include-internal-mappings=services"},
			golden: "console/services-only",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					consoleAPIVersion + " services":            1,
					relationshipsAPIVersion + " relationships": 3,
				}, countItems(items))
				assert.Equal(t, 1+2+3, projectWalkCalls(upstream))
				targets := relationshipTargets(t, items)
				assert.Equal(t, 1, targets[projectURNPrefix], "towards a project item nothing creates")
				assert.Equal(t, 1, targets[revisionURNPrefix], "towards a revision item nothing creates")
			},
		},
		"C6 cluster-project-relationships alone still work": {
			args:   []string{"--include-internal-mappings=cluster-project-relationships"},
			golden: "console/cluster-relationships-only",
			check: func(t *testing.T, items []map[string]any, _ *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{relationshipsAPIVersion + " relationships": 2}, countItems(items))
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, consoleUpstreamDir))
			catalog := newFakeCatalog(t)

			result := runIBDM(t, consoleEnv(upstream, catalog), append([]string{"sync", "console"}, test.args...)...)
			require.Equal(t, 0, result.exitCode, "stderr:\n%s", result.stderr)
			assert.Zero(t, countLogsAtLevel(result.logs, levelError), "stderr:\n%s", result.stderr)

			test.check(t, catalog.received(), upstream)
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}

// serviceNames returns the sorted names of the service items.
func serviceNames(t *testing.T, items []map[string]any) []string {
	t.Helper()

	services := itemsOf(items, consoleAPIVersion, "services")
	names := make([]string, 0, len(services))
	for _, item := range services {
		metadata, ok := item["metadata"].(map[string]any)
		require.True(t, ok)
		title, ok := metadata["title"].(string)
		require.True(t, ok)
		names = append(names, title)
	}
	return names
}

// countLogsAtLevel counts the log records of a level.
func countLogsAtLevel(logs []map[string]any, level string) int {
	count := 0
	for _, record := range logs {
		if record[logLevelKey] == level {
			count++
		}
	}
	return count
}

// TestConsoleWebhook covers ibdm run console end to end, and the webhook claims of the
// dependency section of docs/how-to/050_console-source.md.
func TestConsoleWebhook(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args          []string
		payload       string
		secret        string
		expectedCode  int
		expectedItems int
		golden        string
		check         func(t *testing.T, items []map[string]any, upstream *fakeUpstream, proc *process)
	}{
		"W6 a signed project event is written": {
			args:          []string{"--include-internal-mappings=all"},
			payload:       "project-created-nested.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "console/webhook-project-created",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream, _ *process) {
				t.Helper()
				assert.Equal(t, map[string]int{consoleAPIVersion + " projects": 1}, countItems(items))
				assert.Empty(t, upstream.received(), "a project event is written as received, without any fetch")
			},
		},
		"W7 a configuration event fetches the project and its configuration": {
			args:          []string{"--include-internal-mappings=all"},
			payload:       "configuration-saved.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1 + 1 + 1 + 1 + 1 + 3 + 2,
			golden:        "console/webhook-configuration-saved",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream, _ *process) {
				t.Helper()
				assert.Equal(t, map[string]int{
					consoleAPIVersion + " projects":            1,
					consoleAPIVersion + " revisions":           1,
					consoleAPIVersion + " services":            1,
					consoleAPIVersion + " customresources":     1,
					relationshipsAPIVersion + " relationships": 1 + 3 + 2,
				}, countItems(items))
				assert.Equal(t, 1, upstream.calls(http.MethodGet, "/api/backend/projects/"+storefrontID))
				assert.Equal(t, 1, upstream.calls(http.MethodGet, "/api/backend/projects/"+storefrontID+"/revisions/main/configuration"))
				assertRelationshipsResolve(t, items, nexusURNGroup)
			},
		},
		"W8 a badly signed event is refused": {
			args:         []string{"--include-internal-mappings=all"},
			payload:      "project-created-nested.json",
			secret:       "not-the-webhook-secret",
			expectedCode: http.StatusInternalServerError,
		},
		"W9 the webhook never produces clusters": {
			args:         []string{"--include-internal-mappings=clusters,cluster-project-relationships"},
			payload:      "configuration-saved.json",
			expectedCode: http.StatusNoContent,
		},
		"W10 configuration events are ignored when only projects are selected": {
			args:         []string{"--include-internal-mappings=projects"},
			payload:      "configuration-saved.json",
			expectedCode: http.StatusNoContent,
		},
		"W11 a flat project event payload cannot render the projects mapping, documentsCurrentBehaviour": {
			args:         []string{"--include-internal-mappings=projects"},
			payload:      "project-created-flat.json",
			expectedCode: http.StatusNoContent,
			check: func(t *testing.T, _ []map[string]any, _ *fakeUpstream, proc *process) {
				t.Helper()
				require.Eventually(t, func() bool {
					return countLogs(proc.logs(), levelError, "error applying mapper templates") == 1
				}, webhookTimeout, pollInterval, "the projects mapping reads .project, which a flat payload lacks")
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, consoleUpstreamDir))
			catalog := newFakeCatalog(t)
			proc := startIBDM(t, consoleEnv(upstream, catalog), append([]string{"run", "console"}, test.args...)...)

			secret := test.secret
			if secret == "" {
				secret = webhookSecret
			}
			body := webhookPayload(t, "console", test.payload)
			header := http.Header{consoleSignatureKey: {consoleSignature(body, secret)}}

			require.Equal(t, test.expectedCode, postWebhook(t, proc, consoleWebhookPath, header, body))
			if test.expectedItems == 0 {
				if test.check != nil {
					test.check(t, nil, upstream, proc)
				}
				assertNothingArrives(t, catalog)
				return
			}

			items := catalog.waitFor(t, test.expectedItems, webhookTimeout)
			require.Len(t, items, test.expectedItems)
			test.check(t, items, upstream, proc)
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}
