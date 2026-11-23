// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	jenkinsUpstreamDir           = "testdata/upstream/jenkins"
	jenkinsAPIVersion            = "jenkins.mia-platform.eu/v1"
	jenkinsInstancesFamily       = "instances"
	jenkinsFoldersFamily         = "folders"
	jenkinsMultibranchesFamily   = "multibranches"
	jenkinsWorkflowsFamily       = "workflows"
	jenkinsSharedLibrariesFamily = "sharedlibraries"
	jenkinsCredentialsFamily     = "credentials"
	jenkinsPluginsFamily         = "plugins"
	jenkinsRelationsLabel        = relationshipsAPIVersion + " relationships"
	jenkinsPartOfTypeRef         = "urn:mia-platform-catalog:mia-platform.eu:v1:RelationshipType:part-of.mia-platform.eu"
	jenkinsMyServiceGraphPath    = "/job/my-team/job/my-service/12/stages/tree"
	jenkinsSystemStorePath       = "/credentials/store/system/api/json"
	jenkinsPluginManagerPath     = "/pluginManager/api/json"
	jenkinsURNPrefix             = urnPrefix + "jenkins.mia-platform.eu:v1:"
)

// jenkinsFixtureItem is an item of the upstream fixtures. Its mapping hashes the JSON list of
// parts, with the controller key inserted after the first part. parent names the item its part-of
// relationship points at, empty when it has none.
type jenkinsFixtureItem struct {
	name    string
	urnKind string
	parts   []string
	parent  string
}

// jenkinsFixtureItems lists every item of the upstream fixtures: their identifiers hash the
// address of the fake, so the golden files store placeholders.
var jenkinsFixtureItems = []jenkinsFixtureItem{
	{name: "instance", urnKind: "Instance", parts: []string{"jenkins-instance"}},
	{name: "folder my-team", urnKind: "Folder", parts: []string{"jenkins-folder", "my-team"}, parent: "instance"},
	{name: "folder my-org", urnKind: "Folder", parts: []string{"jenkins-folder", "my-org"}, parent: "instance"},
	{name: "multibranch my-team/my-app", urnKind: "Multibranch", parts: []string{"jenkins-multibranch", "my-team/my-app"}, parent: "folder my-team"},
	{name: "multibranch my-org/my-repo", urnKind: "Multibranch", parts: []string{"jenkins-multibranch", "my-org/my-repo"}, parent: "folder my-org"},
	{name: "workflow my-team/my-service", urnKind: "Workflow", parts: []string{"jenkins-workflow", "my-team/my-service"}, parent: "folder my-team"},
	{name: "workflow root-pipeline", urnKind: "Workflow", parts: []string{"jenkins-workflow", "root-pipeline"}, parent: "instance"},
	{name: "workflow my-team/my-app", urnKind: "Workflow", parts: []string{"jenkins-workflow", "my-team/my-app"}, parent: "multibranch my-team/my-app"},
	{name: "sharedlibrary my-lib", parts: []string{"jenkins-sharedlibrary", "my-lib"}},
	{name: "sharedlibrary other-lib", parts: []string{"jenkins-sharedlibrary", "other-lib"}},
	{name: "credential deploy-key", parts: []string{"jenkins-credential", "system", "", "_", "deploy-key"}},
	{name: "credential team-token", parts: []string{"jenkins-credential", "folder", "my-team", "_", "team-token"}},
	{name: "plugin credentials", urnKind: "Plugin", parts: []string{"jenkins-plugin", "credentials"}, parent: "instance"},
	{name: "plugin pipeline-stage-view", urnKind: "Plugin", parts: []string{"jenkins-plugin", "pipeline-stage-view"}, parent: "instance"},
}

// jenkinsSum returns the hexadecimal SHA-256 of content, as the sha256sum mapping function does.
func jenkinsSum(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// jenkinsIdentifiers computes the identifier of every fixture item and of its part-of
// relationship, as the Jenkins mappings render them for controllerKey, bound to a stable
// placeholder.
func jenkinsIdentifiers(t *testing.T, controllerKey string) map[string]string {
	t.Helper()

	itemIDs := make(map[string]string, len(jenkinsFixtureItems))
	urns := make(map[string]string, len(jenkinsFixtureItems))
	for _, item := range jenkinsFixtureItems {
		parts := append([]string{item.parts[0], controllerKey}, item.parts[1:]...)
		encoded, err := json.Marshal(parts)
		require.NoError(t, err)
		itemIDs[item.name] = jenkinsSum(string(encoded))
		urns[item.name] = jenkinsURNPrefix + item.urnKind + ":" + itemIDs[item.name]
	}

	identifiers := make(map[string]string, 2*len(jenkinsFixtureItems))
	for _, item := range jenkinsFixtureItems {
		identifiers[itemIDs[item.name]] = "<jenkins-id " + item.name + ">"
		if item.parent == "" {
			continue
		}
		relationshipID := jenkinsSum(urns[item.name] + "-" + jenkinsPartOfTypeRef + "-" + urns[item.parent])
		identifiers[relationshipID] = "<jenkins-part-of " + item.name + ">"
	}
	return identifiers
}

// assertJenkinsIdentifiers checks that every item name and every relationship end is an
// identifier computed by jenkinsIdentifiers, and returns the old, new pairs that turn them into
// placeholders.
func assertJenkinsIdentifiers(t *testing.T, items []map[string]any, upstream *fakeUpstream) []string {
	t.Helper()

	identifiers := jenkinsIdentifiers(t, hostOf(t, upstream.baseURL()))
	for _, item := range items {
		assert.Contains(t, identifiers, item["name"], "identifier of %v", item)
		data, ok := item["data"].(map[string]any)
		if !ok || item["itemFamily"] != "relationships" {
			continue
		}
		for _, end := range []string{"sourceRef", "targetRef"} {
			ref, ok := data[end].(string)
			require.True(t, ok, "%s of %v", end, item)
			assert.Contains(t, identifiers, ref[strings.LastIndex(ref, ":")+1:], "%s of %v", end, item)
		}
	}

	replacements := make([]string, 0, 2*len(identifiers))
	for identifier, placeholder := range identifiers {
		replacements = append(replacements, identifier, placeholder)
	}
	return replacements
}

// jenkinsEnv points the Jenkins source at upstream and the Catalog destination at catalog.
func jenkinsEnv(upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
	env := catalog.env()
	env["JENKINS_URL"] = upstream.baseURL()
	env["JENKINS_USERNAME"] = "my-user"
	env["JENKINS_API_TOKEN"] = "test-token"
	return env
}

// jenkinsLabel is the countItems key of the Jenkins item family.
func jenkinsLabel(family string) string {
	return jenkinsAPIVersion + " " + family
}

// jenkinsItem returns the spec of the only item of family whose spec field key equals value.
func jenkinsItem(t *testing.T, items []map[string]any, family, key, value string) map[string]any {
	t.Helper()

	var found []map[string]any
	for _, item := range itemsOf(items, jenkinsAPIVersion, family) {
		spec, ok := item["data"].(map[string]any)
		require.True(t, ok, "item without a spec: %v", item)
		if spec[key] == value {
			found = append(found, spec)
		}
	}
	require.Len(t, found, 1, "one %s item with %s %q", family, key, value)
	return found[0]
}

// TestJenkinsSync covers ibdm sync jenkins, and the dependency claims of the "Internal Mappings
// and Their Dependencies" section of docs/how-to/140_jenkins-source.md.
func TestJenkinsSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args   []string
		golden string
		check  func(t *testing.T, items []map[string]any, upstream *fakeUpstream)
	}{
		"all internal mappings": {
			args:   []string{"--include-internal-mappings=all"},
			golden: "jenkins/all",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					jenkinsLabel(jenkinsInstancesFamily):       1,
					jenkinsLabel(jenkinsFoldersFamily):         2,
					jenkinsLabel(jenkinsMultibranchesFamily):   2,
					jenkinsLabel(jenkinsWorkflowsFamily):       3,
					jenkinsLabel(jenkinsSharedLibrariesFamily): 2,
					jenkinsLabel(jenkinsCredentialsFamily):     2,
					jenkinsLabel(jenkinsPluginsFamily):         2,
					jenkinsRelationsLabel:                      9,
				}, countItems(items), "one part-of relationship per folder, multibranch, workflow and plugin")
				for _, relationship := range itemsOf(items, relationshipsAPIVersion, "relationships") {
					assert.Equal(t, jenkinsPartOfTypeRef, relationship["data"].(map[string]any)["typeRef"])
				}
				assertRelationshipsResolve(t, items)

				myService := jenkinsItem(t, items, jenkinsWorkflowsFamily, "fullName", "my-team/my-service")
				build, ok := myService["build"].(map[string]any)
				require.True(t, ok, "my-service has its last completed build")
				assert.Equal(t, "pipeline-graph-view", build["stagesSource"], "Graph View provides the stages when installed")

				rootPipeline := jenkinsItem(t, items, jenkinsWorkflowsFamily, "fullName", "root-pipeline")
				assert.Nil(t, rootPipeline["build"], "a workflow without completed builds has a null build")

				myRepo := jenkinsItem(t, items, jenkinsMultibranchesFamily, "fullName", "my-org/my-repo")
				for _, key := range []string{"primaryBranch", "branchCount", "pullRequestCount", "tagCount"} {
					assert.Nil(t, myRepo[key], "%s is null for a multibranch project without primary branch and views", key)
				}

				otherLib := jenkinsItem(t, items, jenkinsSharedLibrariesFamily, "name", "other-lib")
				assert.Equal(t, []any{}, otherLib["variables"], "a library without global variables has an empty list")
			},
		},
		"credentials alone walk the folder tree without reading builds": {
			args:   []string{"--include-internal-mappings=credentials"},
			golden: "jenkins/credentials-only",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{jenkinsLabel(jenkinsCredentialsFamily): 2}, countItems(items))
				assert.Equal(t, 1, upstream.calls(http.MethodGet, jenkinsSystemStorePath))
				assert.Zero(t, upstream.calls(http.MethodGet, jenkinsMyServiceGraphPath), "no stage is read")
				assert.Zero(t, upstream.calls(http.MethodGet, jenkinsPluginManagerPath), "the plugins are not read")
			},
		},
		"shared libraries alone read the builds without their stages": {
			args:   []string{"--include-internal-mappings=sharedlibraries"},
			golden: "jenkins/sharedlibraries-only",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{jenkinsLabel(jenkinsSharedLibrariesFamily): 2}, countItems(items))
				assert.Zero(t, upstream.calls(http.MethodGet, jenkinsMyServiceGraphPath), "no stage is read")
				assert.Zero(t, upstream.calls(http.MethodGet, jenkinsPluginManagerPath), "the stage plugins are not detected")
			},
		},
		"workflows alone relate to items the run does not write": {
			args:   []string{"--include-internal-mappings=workflows"},
			golden: "jenkins/workflows-only",
			check: func(t *testing.T, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{
					jenkinsLabel(jenkinsWorkflowsFamily): 3,
					jenkinsRelationsLabel:                3,
				}, countItems(items), "the relationships point at the folder, the multibranch project and the instance")
				assert.Zero(t, upstream.calls(http.MethodGet, jenkinsSystemStorePath), "no credential is read")
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, loadRoutes(t, jenkinsUpstreamDir))
			catalog := newFakeCatalog(t)

			result := runIBDM(t, jenkinsEnv(upstream, catalog), append([]string{"sync", "jenkins"}, test.args...)...)
			require.Equal(t, 0, result.exitCode, "stderr:\n%s", result.stderr)
			assert.Zero(t, countLogsAtLevel(result.logs, levelError), "stderr:\n%s", result.stderr)

			test.check(t, catalog.received(), upstream)
			replacements := assertJenkinsIdentifiers(t, catalog.received(), upstream)
			assertGolden(t, test.golden, catalog, upstream, replacements...)
		})
	}
}

// TestJenkinsRunIsRefused covers the sync only nature of the integration: ibdm run jenkins stops
// before any traffic.
func TestJenkinsRunIsRefused(t *testing.T) {
	t.Parallel()

	upstream := newFakeUpstream(t, nil)
	catalog := newFakeCatalog(t)

	result := runIBDM(t, jenkinsEnv(upstream, catalog), "run", "jenkins", "--include-internal-mappings=all")
	assert.Equal(t, 1, result.exitCode)
	assert.Contains(t, result.stderr, "invalid integration name provided: jenkins")
	assert.Empty(t, upstream.received(), "the controller is never called")
	assert.Empty(t, catalog.received(), "the Catalog receives nothing")
}
