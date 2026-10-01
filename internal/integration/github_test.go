// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"net/http"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	githubUpstreamDir = "testdata/upstream/github"
	githubMappingsDir = "testdata/mappings/github"

	githubAPIVersionHeader = "X-GitHub-Api-Version"
	githubDefaultVersion   = "2026-03-10"
	githubLegacyVersion    = "2022-11-28"

	githubAPIVersion        = "github.mia-platform.eu/v1"
	relationshipsAPIVersion = "mia-platform.eu/v1"
	externalAPIVersion      = "integration.example.com/v1"

	githubReposPath      = "/orgs/acme-widgets/repos"
	githubGizmoRunsPath  = "/repos/acme-widgets/gizmo-service/actions/runs"
	githubGizmoLangsPath = "/repos/acme-widgets/gizmo-service/languages"

	levelError = "error"
)

// githubRepositoryNames are the repositories of the fixtures, over two pages.
var githubRepositoryNames = []string{"cog-docs", "gizmo-service", "sprocket-ui"}

// githubRun is one sync github run against the GitHub fixtures and a fake Catalog.
type githubRun struct {
	result   result
	catalog  *fakeCatalog
	upstream *fakeUpstream
}

// syncGitHub runs ibdm sync github with args against fresh fakes. prepare, when set, configures
// the fake Catalog before the run.
func syncGitHub(t *testing.T, prepare func(*fakeCatalog), args ...string) githubRun {
	t.Helper()

	upstream := newFakeUpstream(t, loadRoutes(t, githubUpstreamDir), githubAPIVersionHeader)
	catalog := newFakeCatalog(t)
	if prepare != nil {
		prepare(catalog)
	}

	run := githubRun{
		result:   runIBDM(t, githubEnv(upstream, catalog), append([]string{"sync", "github"}, args...)...),
		catalog:  catalog,
		upstream: upstream,
	}
	require.Equal(t, 0, run.result.exitCode, "stderr:\n%s", run.result.stderr)
	return run
}

// githubMapping returns the path of a GitHub scenario mapping file or directory.
func githubMapping(name string) string {
	return filepath.Join(githubMappingsDir, name)
}

// itemsOf returns the items of one item type, in arrival order.
func itemsOf(items []map[string]any, apiVersion, itemFamily string) []map[string]any {
	var selected []map[string]any
	for _, item := range items {
		if item["apiVersion"] == apiVersion && item["itemFamily"] == itemFamily {
			selected = append(selected, item)
		}
	}
	return selected
}

// specValues returns the value of key in the spec of every item, sorted as the items are.
func specValues(t *testing.T, items []map[string]any, key string) []any {
	t.Helper()

	values := make([]any, 0, len(items))
	for _, item := range items {
		spec, ok := item["data"].(map[string]any)
		require.True(t, ok, "item without a spec: %v", item)
		values = append(values, spec[key])
	}
	return values
}

// sortedSpecValues returns the string values of key in the spec of every item, sorted.
func sortedSpecValues(t *testing.T, items []map[string]any, key string) []string {
	t.Helper()

	values := make([]string, 0, len(items))
	for _, value := range specValues(t, items, key) {
		text, ok := value.(string)
		require.True(t, ok, "spec %s is not a string: %v", key, value)
		values = append(values, text)
	}
	slices.Sort(values)
	return values
}

// requestsWithVersion counts the requests to path made with the given API version.
func requestsWithVersion(upstream *fakeUpstream, path, version string) int {
	count := 0
	for _, request := range upstream.received() {
		if request.Path == path && request.Header[http.CanonicalHeaderKey(githubAPIVersionHeader)] == version {
			count++
		}
	}
	return count
}

// assertNoErrorLogs fails the test if the run logged an error.
func assertNoErrorLogs(t *testing.T, run githubRun) {
	t.Helper()

	for _, record := range run.result.logs {
		assert.NotEqual(t, levelError, record[logLevelKey], "unexpected error log: %v", record)
	}
}

// TestGitHubSync covers, end to end on GitHub, every behaviour Self-Seeded Mappings introduced
// on the sync path. Scenarios sharing a golden file must produce exactly the same traffic.
func TestGitHubSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args    []string
		prepare func(*fakeCatalog)
		golden  string
		check   func(t *testing.T, run githubRun)
	}{
		"G1 all internal mappings, over two pages": {
			args:   []string{"--include-internal-mappings=all"},
			golden: "github/all",
			check: func(t *testing.T, run githubRun) {
				t.Helper()
				assertNoErrorLogs(t, run)
				items := run.catalog.received()
				assert.Len(t, items, 7, "3 repositories, 2 workflow runs and their 2 relationships")
				assert.Equal(t, githubRepositoryNames, sortedSpecValues(t, itemsOf(items, githubAPIVersion, "repositories"), "name"),
					"the repository of page 2 is there: pagination followed the Link header")
				assert.Len(t, itemsOf(items, githubAPIVersion, "workflowruns"), 2)
				assert.Len(t, itemsOf(items, relationshipsAPIVersion, "relationships"), 2)
				assert.Equal(t, 2, run.upstream.calls(http.MethodGet, githubReposPath), "one listing, two pages")
			},
		},
		"G2 allow-list": {
			args:   []string{"--include-internal-mappings=repositories"},
			golden: "github/repositories-only",
			check: func(t *testing.T, run githubRun) {
				t.Helper()
				assertNoErrorLogs(t, run)
				assert.Len(t, run.catalog.received(), 3)
				assert.Zero(t, run.upstream.calls(http.MethodGet, githubGizmoRunsPath), "workflow runs are not fetched when not selected")
			},
		},
		"G3 include minus exclude is the same as the allow-list": {
			args:   []string{"--include-internal-mappings=all", "--exclude-internal-mappings=workflowruns"},
			golden: "github/repositories-only",
		},
		"G4 an external mapping on an internal type fans out": {
			args:   []string{"--include-internal-mappings=repositories", "-f", githubMapping("fanout")},
			golden: "github/fanout",
			check: func(t *testing.T, run githubRun) {
				t.Helper()
				assertNoErrorLogs(t, run)
				items := run.catalog.received()
				assert.Len(t, itemsOf(items, githubAPIVersion, "repositories"), 3)
				assert.Len(t, itemsOf(items, externalAPIVersion, "repositorycards"), 3)
				assert.Equal(t, 1, run.upstream.calls(http.MethodGet, githubGizmoLangsPath), "both mappings share one fetch: same extra")

				// G11: the resolved set is reported before any work.
				report := "github: using 1 internal mappings and 1 external mappings"
				require.True(t, run.result.hasLog(levelInfo, report), "stderr:\n%s", run.result.stderr)
				for _, record := range run.result.logs {
					if record[logMessageKey] == report {
						assert.Equal(t, []any{"repositories"}, record["internalMappings"])
						assert.Equal(t, []any{"repository-cards"}, record["externalMappings"])
					}
				}
			},
		},
		"G5 each mapping renders its own copy of the payload": {
			args:   []string{"--include-internal-mappings=repositories", "-f", githubMapping("deepcopy")},
			golden: "github/deepcopy",
			check: func(t *testing.T, run githubRun) {
				t.Helper()
				assertNoErrorLogs(t, run)
				items := run.catalog.received()
				assert.Equal(t, []any{"tampered-name", "tampered-name", "tampered-name"}, specValues(t, itemsOf(items, externalAPIVersion, "tamperedrepositories"), "name"))
				assert.Equal(t, githubRepositoryNames, sortedSpecValues(t, itemsOf(items, externalAPIVersion, "observedrepositories"), "name"),
					"the mapping loaded after the tamperer sees the names the source sent")
				assert.Equal(t, githubRepositoryNames, sortedSpecValues(t, itemsOf(items, githubAPIVersion, "repositories"), "name"))
			},
		},
		"G6 a different extra apiVersion makes a second listing": {
			args:   []string{"--include-internal-mappings=repositories", "-f", githubMapping("extra-version")},
			golden: "github/extra-version",
			check: func(t *testing.T, run githubRun) {
				t.Helper()
				assertNoErrorLogs(t, run)
				assert.Equal(t, 2, requestsWithVersion(run.upstream, githubReposPath, githubDefaultVersion), "the internal mapping's listing, two pages")
				assert.Equal(t, 2, requestsWithVersion(run.upstream, githubReposPath, githubLegacyVersion), "the external mapping's listing, two pages")
				assert.True(t, run.result.hasLog(levelWarn, "external mapping declares a root extra different from the internal mapping of its type, the source fetches its data separately"))

				items := run.catalog.received()
				assert.Equal(t, []string{"Cog documentation", "Gizmo service", "Sprocket user interface"},
					sortedSpecValues(t, itemsOf(items, githubAPIVersion, "repositories"), "description"))
				assert.Equal(t, []string{
					"Cog documentation (as listed by API version 2022-11-28)",
					"Gizmo service (as listed by API version 2022-11-28)",
					"Sprocket user interface (as listed by API version 2022-11-28)",
				}, sortedSpecValues(t, itemsOf(items, externalAPIVersion, "legacyrepositories"), "description"))
			},
		},
		"G7 a root createIf declines payloads": {
			args:   []string{"--include-internal-mappings=repositories", "-f", githubMapping("createif")},
			golden: "github/createif",
			check: func(t *testing.T, run githubRun) {
				t.Helper()
				assertNoErrorLogs(t, run)
				items := run.catalog.received()
				assert.Equal(t, []string{"cog-docs", "gizmo-service"}, sortedSpecValues(t, itemsOf(items, externalAPIVersion, "activerepositories"), "name"),
					"the archived repository is declined")
				assert.Len(t, itemsOf(items, githubAPIVersion, "repositories"), 3, "the other mapping is not affected")
			},
		},
		"G8 sync skips a mapping declared not syncable": {
			args:   []string{"--include-internal-mappings=repositories", "-f", githubMapping("not-syncable")},
			golden: "github/repositories-only",
			check: func(t *testing.T, run githubRun) {
				t.Helper()
				assertNoErrorLogs(t, run)
				report := "github: using 1 internal mappings and 0 external mappings"
				require.True(t, run.result.hasLog(levelInfo, report), "stderr:\n%s", run.result.stderr)
				for _, record := range run.result.logs {
					if record[logMessageKey] == report {
						assert.Equal(t, []any{"stream-only-repositories"}, record["skippedNotSyncable"])
					}
				}
			},
		},
		"G9 a mapping failing on a payload does not stop the others": {
			args:   []string{"--include-internal-mappings=repositories", "-f", githubMapping("failing")},
			golden: "github/failing",
			check: func(t *testing.T, run githubRun) {
				t.Helper()
				items := run.catalog.received()
				assert.Len(t, itemsOf(items, externalAPIVersion, "repositoryhomepages"), 2, "cog-docs has no homepage")
				assert.Len(t, itemsOf(items, githubAPIVersion, "repositories"), 3)
				assert.Equal(t, 1, countLogs(run.result.logs, levelError, "error applying mapper templates"))
			},
		},
		"G10 shared item types start when allowed": {
			args: []string{
				"--include-internal-mappings=repositories", "--allow-shared-item-types",
				"-f", cliMapping("shared-item-type-a.yaml"), "-f", cliMapping("shared-item-type-b.yaml"),
			},
			golden: "github/shared-item-types",
			check: func(t *testing.T, run githubRun) {
				t.Helper()
				assertNoErrorLogs(t, run)
				assert.True(t, run.result.hasLog(levelWarn, "mappings write the same item type, their items may overwrite each other"))
				assert.Len(t, itemsOf(run.catalog.received(), externalAPIVersion, "repositories"), 6, "both mappings write every repository")
			},
		},
		"G12 a Catalog failure only affects its item": {
			args:    []string{"--include-internal-mappings=all"},
			prepare: failSprocketRepository,
			golden:  "github/all",
			check: func(t *testing.T, run githubRun) {
				t.Helper()
				assert.Len(t, run.catalog.received(), 7, "every item is still sent; the refused one is recorded too")
				assert.Equal(t, 1, countLogs(run.result.logs, levelError, "error sending data to destination"),
					"the failure is logged, and the sync still exits 0: documents the current behaviour")
			},
		},
		"G14 workflow runs alone list the repositories without writing them": {
			args:   []string{"--include-internal-mappings=workflowruns"},
			golden: "github/workflowruns-only",
			check: func(t *testing.T, run githubRun) {
				t.Helper()
				assertNoErrorLogs(t, run)
				items := run.catalog.received()
				assert.Empty(t, itemsOf(items, githubAPIVersion, "repositories"))
				assert.Len(t, itemsOf(items, githubAPIVersion, "workflowruns"), 2)
				assert.Equal(t, 2, run.upstream.calls(http.MethodGet, githubReposPath), "one listing, two pages")
				assert.Zero(t, run.upstream.calls(http.MethodGet, githubGizmoLangsPath), "languages are only fetched for repository items")
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			run := syncGitHub(t, test.prepare, test.args...)
			if test.check != nil {
				test.check(t, run)
			}
			assertGolden(t, test.golden, run.catalog, run.upstream)
		})
	}
}

// failSprocketRepository makes the fake Catalog refuse the internal item of sprocket-ui.
func failSprocketRepository(catalog *fakeCatalog) {
	catalog.failOn(func(item map[string]any) bool {
		spec, _ := item["data"].(map[string]any)
		return item["apiVersion"] == githubAPIVersion && item["itemFamily"] == "repositories" && spec["name"] == "sprocket-ui"
	}, http.StatusInternalServerError)
}
