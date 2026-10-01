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
	gitlabUpstreamDir = "testdata/upstream/gitlab"
	gitlabWebhookPath = "/gitlab/webhook"

	gitlabTokenHeader = "X-Gitlab-Token"
	gitlabEventHeader = "X-Gitlab-Event"

	gitlabAPIVersion = "gitlab.mia-platform.eu/v1"

	gitlabProjectsPath     = "/api/v4/projects"
	gitlabGroupsPath       = "/api/v4/groups"
	gitlabProject101Path   = "/api/v4/projects/101"
	gitlabLanguages101Path = "/api/v4/projects/101/languages"
	gitlabPipeline5001Path = "/api/v4/projects/101/pipelines/5001"

	gitlabSkippedGroupTokensLog = "skipping group access tokens: insufficient permissions"
)

// gitlabEnv points the GitLab source at upstream, and the Catalog destination at catalog.
func gitlabEnv(upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
	env := catalog.env()
	env["GITLAB_BASE_URL"] = upstream.baseURL()
	env["GITLAB_TOKEN"] = "test-token"
	env["GITLAB_WEBHOOK_TOKEN"] = webhookSecret
	return env
}

// gitlabCallsUnder counts the requests whose path starts with prefix.
func gitlabCallsUnder(upstream *fakeUpstream, prefix string) int {
	count := 0
	for _, request := range upstream.received() {
		if strings.HasPrefix(request.Path, prefix) {
			count++
		}
	}
	return count
}

// gitlabResourceTypes returns the sorted resourceType of the access token items.
func gitlabResourceTypes(t *testing.T, items []map[string]any) []string {
	t.Helper()
	return sortedSpecValues(t, itemsOf(items, gitlabAPIVersion, "accesstokens"), "resourceType")
}

// TestGitLabSync covers ibdm sync gitlab, and checks the claims of the "Sync Mode" and
// "Internal Mappings" sections of docs/how-to/070_gitlab-source.md.
func TestGitLabSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args             []string
		routes           func(t *testing.T) []route
		expectedExitCode int
		golden           string
		check            func(t *testing.T, result result, items []map[string]any, upstream *fakeUpstream)
	}{
		"GL1 all internal mappings, over two pages": {
			args:   []string{"--include-internal-mappings=all"},
			golden: "gitlab/all",
			check: func(t *testing.T, result result, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					gitlabAPIVersion + " projects":             2,
					gitlabAPIVersion + " pipelines":            2,
					gitlabAPIVersion + " accesstokens":         2,
					relationshipsAPIVersion + " relationships": 2 + 1,
				}, countItems(items), "relationships: 2 pipeline→project, 1 project token→project; the group token has none")
				assert.Equal(t, []string{"group", "project"}, gitlabResourceTypes(t, items))
				assert.Equal(t, 2, upstream.calls(http.MethodGet, gitlabProjectsPath), "both pages, driven by x-total-pages")
				assert.Equal(t, 1, countLogs(result.logs, levelError, gitlabSkippedGroupTokensLog), "the 403 group is skipped")
				assert.Equal(t, 1, countLogsAtLevel(result.logs, levelError), "and that is the only error")
				assertRelationshipsResolve(t, items)
			},
		},
		"GL2 projects alone fetch projects and their languages only": {
			args:   []string{"--include-internal-mappings=projects"},
			golden: "gitlab/projects-only",
			check: func(t *testing.T, result result, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{gitlabAPIVersion + " projects": 2}, countItems(items))
				assert.Len(t, upstream.received(), 2+2, "two listing pages and two languages requests")
				assert.Zero(t, gitlabCallsUnder(upstream, gitlabGroupsPath), "no group walk without access tokens")
				assert.Zero(t, countLogsAtLevel(result.logs, levelError))
			},
		},
		"GL3 pipelines alone produce nothing": {
			args:   []string{"--include-internal-mappings=pipelines"},
			golden: "gitlab/pipelines-only",
			check: func(t *testing.T, result result, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Empty(t, items, "pipelines are only fetched while the projects are walked")
				assert.Empty(t, upstream.received(), "not even the project walk runs")
				assert.True(t, result.hasLog(levelInfo, "gitlab: using 1 internal mappings and 0 external mappings"),
					"the run starts normally, with no warning that the selection produces nothing")
				assert.Zero(t, countLogsAtLevel(result.logs, levelWarn), "no warning that the selection produces nothing")
			},
		},
		"GL4 the documented projects and pipelines selection": {
			args:   []string{"--include-internal-mappings=projects,pipelines"},
			golden: "gitlab/projects-pipelines",
			check: func(t *testing.T, result result, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					gitlabAPIVersion + " projects":             2,
					gitlabAPIVersion + " pipelines":            2,
					relationshipsAPIVersion + " relationships": 2,
				}, countItems(items))
				assert.Zero(t, gitlabCallsUnder(upstream, gitlabGroupsPath))
				assertRelationshipsResolve(t, items)
				assert.Zero(t, countLogsAtLevel(result.logs, levelError))
			},
		},
		"GL5 access tokens alone walk the groups only": {
			args:   []string{"--include-internal-mappings=accesstokens"},
			golden: "gitlab/accesstokens-only",
			check: func(t *testing.T, _ result, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{gitlabAPIVersion + " accesstokens": 1}, countItems(items),
					"project tokens are fetched only during the project walk")
				assert.Equal(t, []string{"group"}, gitlabResourceTypes(t, items))
				assert.Zero(t, upstream.calls(http.MethodGet, gitlabProjectsPath), "no project walk")
			},
		},
		"GL6 a failing languages call aborts the sync, documentsCurrentBehaviour": {
			args: []string{"--include-internal-mappings=projects"},
			routes: func(t *testing.T) []route {
				t.Helper()
				failing := route{Method: http.MethodGet, Path: "/api/v4/projects/102/languages", Status: http.StatusInternalServerError}
				return append([]route{failing}, loadRoutes(t, gitlabUpstreamDir)...)
			},
			expectedExitCode: 1,
			golden:           "gitlab/languages-failure",
			check: func(t *testing.T, result result, items []map[string]any, _ *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{gitlabAPIVersion + " projects": 1}, countItems(items),
					"the project of page 1 was sent before the failure; the one of page 2 is lost")
				assert.Equal(t, 1, countLogs(result.logs, levelError, "error fetching project languages, proceeding with empty languages"),
					"the message says the sync proceeds, but it stops")
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			routes := test.routes
			if routes == nil {
				routes = func(t *testing.T) []route {
					t.Helper()
					return loadRoutes(t, gitlabUpstreamDir)
				}
			}
			upstream := newFakeUpstream(t, routes(t))
			catalog := newFakeCatalog(t)

			result := runIBDM(t, gitlabEnv(upstream, catalog), append([]string{"sync", "gitlab"}, test.args...)...)
			require.Equal(t, test.expectedExitCode, result.exitCode, "stderr:\n%s", result.stderr)

			test.check(t, result, catalog.received(), upstream)
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}

// TestGitLabWebhook covers ibdm run gitlab end to end, and the claims of the "Webhook Mode"
// section of docs/how-to/070_gitlab-source.md.
func TestGitLabWebhook(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args          []string
		event         string
		payload       string
		token         string
		expectedCode  int
		expectedItems int
		golden        string
		check         func(t *testing.T, items []map[string]any, upstream *fakeUpstream, proc *process)
	}{
		"GLW1 a pipeline hook writes the project and the pipeline": {
			args:          []string{"--include-internal-mappings=all"},
			event:         "Pipeline Hook",
			payload:       "pipeline-hook.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 3,
			golden:        "gitlab/webhook-pipeline-hook",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream, _ *process) {
				t.Helper()
				assert.Equal(t, map[string]int{
					gitlabAPIVersion + " projects":             1,
					gitlabAPIVersion + " pipelines":            1,
					relationshipsAPIVersion + " relationships": 1,
				}, countItems(items))
				assert.Equal(t, 1, upstream.calls(http.MethodGet, gitlabProject101Path))
				assert.Equal(t, 1, upstream.calls(http.MethodGet, gitlabLanguages101Path))
				assert.Equal(t, 1, upstream.calls(http.MethodGet, gitlabPipeline5001Path))
				assertRelationshipsResolve(t, items)
			},
		},
		"GLW2 a push hook writes the project": {
			args:          []string{"--include-internal-mappings=all"},
			event:         "Push Hook",
			payload:       "push-hook.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "gitlab/webhook-push-hook",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream, _ *process) {
				t.Helper()
				assert.Equal(t, map[string]int{gitlabAPIVersion + " projects": 1}, countItems(items))
				assert.Equal(t, 1, upstream.calls(http.MethodGet, gitlabProject101Path))
				assert.Equal(t, 1, upstream.calls(http.MethodGet, gitlabLanguages101Path))
			},
		},
		"GLW3 a wrong token is refused": {
			args:         []string{"--include-internal-mappings=all"},
			event:        "Push Hook",
			payload:      "push-hook.json",
			token:        "not-the-webhook-token",
			expectedCode: http.StatusInternalServerError,
			check: func(t *testing.T, _ []map[string]any, upstream *fakeUpstream, _ *process) {
				t.Helper()
				assert.Empty(t, upstream.received())
			},
		},
		"GLW4 a pipeline hook with projects alone writes nothing, documentsCurrentBehaviour": {
			args:         []string{"--include-internal-mappings=projects"},
			event:        "Pipeline Hook",
			payload:      "pipeline-hook.json",
			expectedCode: http.StatusNoContent,
			check: func(t *testing.T, _ []map[string]any, upstream *fakeUpstream, _ *process) {
				t.Helper()
				require.Eventually(t, func() bool {
					return upstream.calls(http.MethodGet, gitlabPipeline5001Path) == 1
				}, webhookTimeout, pollInterval, "the event is fully fetched before the selection is checked")
			},
		},
		"GLW5 a pipeline hook with pipelines alone writes nothing": {
			args:         []string{"--include-internal-mappings=pipelines"},
			event:        "Pipeline Hook",
			payload:      "pipeline-hook.json",
			expectedCode: http.StatusNoContent,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, gitlabUpstreamDir))
			catalog := newFakeCatalog(t)
			proc := startIBDM(t, gitlabEnv(upstream, catalog), append([]string{"run", "gitlab"}, test.args...)...)

			token := test.token
			if token == "" {
				token = webhookSecret
			}
			body := webhookPayload(t, "gitlab", test.payload)
			header := http.Header{gitlabEventHeader: {test.event}, gitlabTokenHeader: {token}}

			require.Equal(t, test.expectedCode, postWebhook(t, proc, gitlabWebhookPath, header, body))
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
