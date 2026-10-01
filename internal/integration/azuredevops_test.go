// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	azuredevopsUpstreamDir = "testdata/upstream/azuredevops"
	azuredevopsMappingsDir = "testdata/mappings/azuredevops"
	azuredevopsWebhookPath = "/azure-devops/webhook"

	azuredevopsAPIVersion = "azuredevops.mia-platform.eu/v1"

	azuredevopsRepositoriesPath = "/acme-widgets/_apis/git/repositories"
	azuredevopsTeamsPath        = "/acme-widgets/_apis/teams"

	azuredevopsWebhookUser     = "ibdm-hook"
	azuredevopsWebhookPassword = "test-hook-password"
)

// azuredevopsEnv points the Azure DevOps source at the organization acme-widgets of upstream,
// and the Catalog destination at catalog.
func azuredevopsEnv(upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
	env := catalog.env()
	env["AZURE_DEVOPS_ORGANIZATION_URL"] = upstream.baseURL() + "/acme-widgets"
	env["AZURE_DEVOPS_PERSONAL_TOKEN"] = "test-token"
	return env
}

// azuredevopsBasicAuth returns the Authorization header value Azure DevOps sends for a webhook
// registered with user and password.
func azuredevopsBasicAuth(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// azuredevopsNames returns the sorted spec names of items.
func azuredevopsNames(t *testing.T, items []map[string]any) []string {
	t.Helper()
	return sortedSpecValues(t, items, "name")
}

// TestAzureDevOpsSync covers ibdm sync azure-devops, and the claims of the dependency section of
// docs/how-to/060_azuredevops-source.md.
func TestAzureDevOpsSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args   []string
		golden string
		check  func(t *testing.T, items []map[string]any, upstream *fakeUpstream)
	}{
		"all internal mappings, over two continuation pages": {
			args:   []string{"--include-internal-mappings=all"},
			golden: "azuredevops/all",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					azuredevopsAPIVersion + " gitrepositories": 3,
					azuredevopsAPIVersion + " teams":           2,
				}, countItems(items), "no internal mapping creates relationships")
				assert.Equal(t, []string{"storefront-api", "storefront-archive", "storefront-web"},
					azuredevopsNames(t, itemsOf(items, azuredevopsAPIVersion, "gitrepositories")),
					"the repository of page 2 is there: the continuation token was followed")
				assert.Equal(t, 2, upstream.calls(http.MethodGet, azuredevopsRepositoriesPath))
				assert.Equal(t, 1, upstream.calls(http.MethodGet, azuredevopsTeamsPath))
				assertRelationshipsResolve(t, items)
			},
		},
		"teams alone read only the teams": {
			args:   []string{"--include-internal-mappings=teams"},
			golden: "azuredevops/teams-only",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{azuredevopsAPIVersion + " teams": 2}, countItems(items))
				assert.Zero(t, upstream.calls(http.MethodGet, azuredevopsRepositoriesPath), "each selected type is read on its own")
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, azuredevopsUpstreamDir), "Accept")
			catalog := newFakeCatalog(t)

			result := runIBDM(t, azuredevopsEnv(upstream, catalog), append([]string{"sync", "azure-devops"}, test.args...)...)
			require.Equal(t, 0, result.exitCode, "stderr:\n%s", result.stderr)
			assert.Zero(t, countLogsAtLevel(result.logs, levelError), "stderr:\n%s", result.stderr)

			test.check(t, catalog.received(), upstream)
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}

// TestAzureDevOpsSyncErrorStatusDocumentsCurrentBehaviour documents that the source does not
// check the response status: an error answer decodes as a page without items, and the sync ends
// successfully with nothing written and no error logged (plan §8.2, B-4).
func TestAzureDevOpsSyncErrorStatusDocumentsCurrentBehaviour(t *testing.T) {
	t.Parallel()

	errorBody, err := os.ReadFile(filepath.Join(azuredevopsUpstreamDir, "error.json"))
	require.NoError(t, err)
	upstream := newFakeUpstream(t, []route{{
		Method: http.MethodGet,
		Path:   azuredevopsTeamsPath,
		Status: http.StatusUnauthorized,
		body:   errorBody,
	}})
	catalog := newFakeCatalog(t)

	result := runIBDM(t, azuredevopsEnv(upstream, catalog), "sync", "azure-devops", "--include-internal-mappings=teams")

	require.Equal(t, 0, result.exitCode, "stderr:\n%s", result.stderr)
	assert.Zero(t, countLogsAtLevel(result.logs, levelError), "the 401 is not reported")
	assert.Equal(t, 1, upstream.calls(http.MethodGet, azuredevopsTeamsPath))
	assert.Empty(t, catalog.received())
}

// TestAzureDevOpsWebhook covers ibdm run azure-devops: authentication, upsert and delete events,
// and the dispatch of one event to every type whose mappings list it (Layer C targeting).
func TestAzureDevOpsWebhook(t *testing.T) {
	t.Parallel()

	authEnv := map[string]string{
		"AZURE_DEVOPS_WEBHOOK_USER":     azuredevopsWebhookUser,
		"AZURE_DEVOPS_WEBHOOK_PASSWORD": azuredevopsWebhookPassword,
	}

	testCases := map[string]struct {
		args          []string
		env           map[string]string
		authorization string
		payload       string
		expectedCode  int
		expectedItems int
		golden        string
		check         func(t *testing.T, items []map[string]any)
	}{
		"an authenticated git.repo.created event is written, and teams do not receive it": {
			args:          []string{"--include-internal-mappings=all"},
			env:           authEnv,
			authorization: azuredevopsBasicAuth(azuredevopsWebhookUser, azuredevopsWebhookPassword),
			payload:       "git-repo-created.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "azuredevops/webhook-repo-created",
			check: func(t *testing.T, items []map[string]any) {
				t.Helper()
				assert.Equal(t, map[string]int{azuredevopsAPIVersion + " gitrepositories": 1}, countItems(items),
					"teams list no eventNames, so the event never reaches them")
				assert.Equal(t, "upsert", items[0]["operation"])
				assert.Equal(t, []string{"storefront-mobile"}, azuredevopsNames(t, items), "the repository of the resource is mapped")
			},
		},
		"a git.repo.deleted event deletes the item": {
			args:          []string{"--include-internal-mappings=gitrepositories"},
			payload:       "git-repo-deleted.json",
			expectedCode:  http.StatusNoContent,
			expectedItems: 1,
			golden:        "azuredevops/webhook-repo-deleted",
			check: func(t *testing.T, items []map[string]any) {
				t.Helper()
				assert.Equal(t, "delete", items[0]["operation"])
				assert.NotContains(t, items[0], "data")
			},
		},
		"wrong credentials are refused": {
			args:          []string{"--include-internal-mappings=all"},
			env:           authEnv,
			authorization: azuredevopsBasicAuth(azuredevopsWebhookUser, "not-the-password"),
			payload:       "git-repo-created.json",
			expectedCode:  http.StatusInternalServerError,
		},
		"missing credentials are refused when the webhook has some": {
			args:         []string{"--include-internal-mappings=all"},
			env:          authEnv,
			payload:      "git-repo-created.json",
			expectedCode: http.StatusInternalServerError,
		},
		"an event no selected mapping lists is ignored": {
			args:         []string{"--include-internal-mappings=teams"},
			payload:      "git-repo-created.json",
			expectedCode: http.StatusNoContent,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, nil)
			catalog := newFakeCatalog(t)
			env := azuredevopsEnv(upstream, catalog)
			for key, value := range test.env {
				env[key] = value
			}
			proc := startIBDM(t, env, append([]string{"run", "azure-devops"}, test.args...)...)

			header := http.Header{}
			if test.authorization != "" {
				header.Set("Authorization", test.authorization)
			}
			body := webhookPayload(t, "azuredevops", test.payload)

			require.Equal(t, test.expectedCode, postWebhook(t, proc, azuredevopsWebhookPath, header, body))
			if test.expectedItems == 0 {
				assertNothingArrives(t, catalog)
				return
			}

			items := catalog.waitFor(t, test.expectedItems, webhookTimeout)
			require.Len(t, items, test.expectedItems)
			test.check(t, items)
			assert.Empty(t, upstream.received(), "a webhook event is mapped as received, without any fetch")
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}

// TestAzureDevOpsWebhookTargeting covers Layer C targeting end to end: an external mapping of the
// team type lists git.repo.created, so the event reaches both the gitrepository and the team
// types, in lexical order of type, and the internal teams mapping, which lists no event, does
// not render it.
func TestAzureDevOpsWebhookTargeting(t *testing.T) {
	t.Parallel()

	upstream := newFakeUpstream(t, nil)
	catalog := newFakeCatalog(t)
	proc := startIBDM(t, azuredevopsEnv(upstream, catalog),
		"run", "azure-devops", "--include-internal-mappings=all", "-f", filepath.Join(azuredevopsMappingsDir, "targeting"))

	body := webhookPayload(t, "azuredevops", "git-repo-created.json")
	require.Equal(t, http.StatusNoContent, postWebhook(t, proc, azuredevopsWebhookPath, http.Header{}, body))

	items := catalog.waitFor(t, 2, webhookTimeout)
	require.Len(t, items, 2)
	assert.Equal(t, azuredevopsAPIVersion, items[0]["apiVersion"], "gitrepository is dispatched first")
	assert.Equal(t, "gitrepositories", items[0]["itemFamily"])
	assert.Equal(t, externalAPIVersion, items[1]["apiVersion"], "then team, through the external mapping only")
	assert.Equal(t, "repositorycreations", items[1]["itemFamily"])
	assert.Equal(t, []any{"storefront-mobile"}, specValues(t, items[1:], "repositoryName"))
	assert.Equal(t, []any{"ada@example.com"}, specValues(t, items[1:], "initiatedBy"), "the team type receives the whole resource")
	assert.Empty(t, itemsOf(items, azuredevopsAPIVersion, "teams"), "the internal teams mapping lists no event")
	assert.Equal(t, 1, countLogs(proc.logs(), levelWarn, "external mapping declares a root extra different from the internal mapping of its type, the source fetches its data separately"))
	assertGolden(t, "azuredevops/webhook-targeting", catalog, upstream)
}
