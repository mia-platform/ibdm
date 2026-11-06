// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

var allTypes = []string{instanceType, folderType, multibranchType, workflowType, sharedLibraryType, credentialType, pluginType}

func TestStartSyncProcessEmitsEveryType(t *testing.T) {
	t.Parallel()

	fake := newFakeJenkins(fixtureRoutes())
	src := newTestSource(t, fake)
	rootURL := src.client.baseURL + "/"

	typesToSync := make(map[string]source.MappingExtras)
	for _, dataType := range allTypes {
		typesToSync[dataType] = source.MappingExtras{}
	}

	results := make(chan source.Data, 100)
	require.NoError(t, src.StartSyncProcess(t.Context(), typesToSync, results))
	close(results)

	data := collectData(t, results)
	for _, d := range data {
		assert.Equal(t, source.DataOperationUpsert, d.Operation)
		assert.Equal(t, testFixedTime, d.Time)
	}

	grouped := byType(t, data)
	require.Len(t, grouped[instanceType], 1)
	require.Len(t, grouped[folderType], 2)
	require.Len(t, grouped[multibranchType], 2)
	require.Len(t, grouped[workflowType], 3)
	require.Len(t, grouped[sharedLibraryType], 2)
	require.Len(t, grouped[credentialType], 2)
	require.Len(t, grouped[pluginType], 2)

	t.Run("instance", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, map[string]any{
			"instance": map[string]any{
				keyURL:        rootURL,
				"version":     "2.555.1",
				"description": "Welcome to the my-org build controller.",
				"mode":        "NORMAL",
			},
		}, grouped[instanceType][0].Values)
	})

	t.Run("folders", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, map[string]any{
			"folder": map[string]any{
				keyName: "my-team", keyFullName: "my-team", "displayName": "My Team", "description": "Pipelines of my team",
				keyURL: "https://ci.example.com/job/my-team/", keyKind: folderKindFolder, "objectUrl": "",
			},
			"parent": map[string]any{keyKind: parentKindInstance, keyFullName: "", keyURL: rootURL},
		}, grouped[folderType][0].Values)
		assert.Equal(t, map[string]any{
			"folder": map[string]any{
				keyName: "my-org", keyFullName: "my-org", "displayName": "my-org", "description": "",
				keyURL: "https://ci.example.com/job/my-org/", keyKind: folderKindOrganization, "objectUrl": "https://git.example.com/my-org",
			},
			"parent": map[string]any{keyKind: parentKindInstance, keyFullName: "", keyURL: rootURL},
		}, grouped[folderType][1].Values)
	})

	t.Run("multibranch projects", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, map[string]any{
			"multibranch": map[string]any{
				keyName: "my-app", keyFullName: "my-team/my-app", "displayName": "my-app", "description": "Application repository",
				keyURL:          "https://ci.example.com/job/my-team/job/my-app/",
				"health":        []map[string]any{{"score": 80, "description": "Build stability: 1 out of the last 5 builds failed."}},
				"repositoryUrl": "https://git.example.com/my-org/my-app",
				"primaryBranch": map[string]any{
					keyName: "main", keyFullName: "my-team/my-app/main", keyURL: "https://ci.example.com/job/my-team/job/my-app/job/main/",
				},
				"branchCount":      2,
				"pullRequestCount": 1,
				"tagCount":         0,
			},
			"parent": map[string]any{keyKind: parentKindFolder, keyFullName: "my-team", keyURL: "https://ci.example.com/job/my-team/"},
		}, grouped[multibranchType][0].Values)
		assert.Equal(t, map[string]any{
			"multibranch": map[string]any{
				keyName: "my-repo", keyFullName: "my-org/my-repo", "displayName": "my-repo", "description": "",
				keyURL:          "https://ci.example.com/job/my-org/job/my-repo/",
				"health":        []map[string]any{},
				"repositoryUrl": "",
			},
			"parent": map[string]any{keyKind: parentKindFolder, keyFullName: "my-org", keyURL: "https://ci.example.com/job/my-org/"},
		}, grouped[multibranchType][1].Values)
	})

	t.Run("standalone workflow with failed stage", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, map[string]any{
			"workflow": map[string]any{
				keyName: "my-service", keyFullName: "my-team/my-service", "displayName": "my-service", "description": "Service pipeline",
				keyURL: "https://ci.example.com/job/my-team/job/my-service/", "buildable": true, keyKind: workflowKindStandalone,
			},
			"libraries": []map[string]any{{keyName: "my-lib", "version": "main", "trusted": true}},
			"parent":    map[string]any{keyKind: parentKindFolder, keyFullName: "my-team", keyURL: "https://ci.example.com/job/my-team/"},
			"build": map[string]any{
				"number": int64(12), "result": "FAILURE", "timestamp": int64(1767261600000), "duration": int64(95000),
				keyURL:         "https://ci.example.com/job/my-team/job/my-service/12/",
				"stagesSource": stageViewPluginName,
				"stages": []map[string]any{
					{
						"id": "6", keyName: "Build", "status": "success", "startTimeMillis": int64(1767261601000), "durationMillis": int64(30000),
						"agent": "agent-1", "parallel": false, "synthetic": false, "children": []string{},
					},
					{
						"id": "15", keyName: "Test", "status": "failure", "startTimeMillis": int64(1767261631000), "durationMillis": int64(60000),
						"agent": "agent-1", "parallel": false, "synthetic": false, "children": []string{},
						"failure": map[string]any{
							"message":    "script returned exit code 1",
							"type":       "hudson.AbortException",
							"nodeName":   "Shell Script",
							"logUrl":     "https://ci.example.com/job/my-team/job/my-service/12/execution/node/17/log/",
							"consoleUrl": "https://ci.example.com/job/my-team/job/my-service/12/console",
						},
					},
					{
						"id": "25", keyName: "Deploy", "status": "skipped", "startTimeMillis": int64(1767261691000), "durationMillis": int64(0),
						"agent": "", "parallel": false, "synthetic": false, "children": []string{},
					},
				},
			},
		}, grouped[workflowType][0].Values)
	})

	t.Run("primary branch workflow", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, map[string]any{
			"workflow": map[string]any{
				keyName: "main", keyFullName: "my-team/my-app/main", "displayName": "main", "description": "",
				keyURL: "https://ci.example.com/job/my-team/job/my-app/job/main/", "buildable": true, keyKind: workflowKindPrimaryBranch,
			},
			"libraries": []map[string]any{
				{keyName: "my-lib", "version": "v2", "trusted": true},
				{keyName: "other-lib", "version": "1.0", "trusted": false},
			},
			"multibranch": map[string]any{keyName: "my-app", keyFullName: "my-team/my-app", keyURL: "https://ci.example.com/job/my-team/job/my-app/"},
			"build": map[string]any{
				"number": int64(40), "result": "SUCCESS", "timestamp": int64(1767265200000), "duration": int64(42000),
				keyURL:         "https://ci.example.com/job/my-team/job/my-app/job/main/40/",
				"stagesSource": stageViewPluginName,
				"stages": []map[string]any{
					{
						"id": "5", keyName: "Build", "status": "success", "startTimeMillis": int64(1767265201000), "durationMillis": int64(41000),
						"agent": "", "parallel": false, "synthetic": false, "children": []string{},
					},
				},
			},
		}, grouped[workflowType][1].Values)
	})

	t.Run("workflow without completed builds", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, map[string]any{
			"workflow": map[string]any{
				keyName: "root-pipeline", keyFullName: "root-pipeline", "displayName": "root-pipeline", "description": "",
				keyURL: "https://ci.example.com/job/root-pipeline/", "buildable": true, keyKind: workflowKindStandalone,
			},
			"libraries": []map[string]any{},
			"parent":    map[string]any{keyKind: parentKindInstance, keyFullName: "", keyURL: rootURL},
		}, grouped[workflowType][2].Values)
	})

	t.Run("shared libraries", func(t *testing.T) {
		t.Parallel()
		multibranch := map[string]any{keyName: "my-app", keyFullName: "my-team/my-app", keyURL: "https://ci.example.com/job/my-team/job/my-app/"}
		assert.Equal(t, map[string]any{
			"library": map[string]any{keyName: "my-lib", "versions": []string{"main", "v2"}, "variables": []string{"buildApp", "deployApp"}},
			"usages": []map[string]any{
				{
					keyKind: workflowKindStandalone, keyFullName: "my-team/my-service", keyURL: "https://ci.example.com/job/my-team/job/my-service/",
					"version": "main", "trusted": true, "changelog": true, "buildNumber": int64(12),
				},
				{
					keyKind: workflowKindPrimaryBranch, keyFullName: "my-team/my-app/main", keyURL: "https://ci.example.com/job/my-team/job/my-app/job/main/",
					"version": "v2", "trusted": true, "changelog": true, "buildNumber": int64(40), "multibranch": multibranch,
				},
			},
		}, grouped[sharedLibraryType][0].Values)
		assert.Equal(t, "other-lib", grouped[sharedLibraryType][1].Values["library"].(map[string]any)[keyName])
	})

	t.Run("credentials", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, map[string]any{
			"credential": map[string]any{
				"id": "deploy-key", "displayName": "deploy (Deploy key)", "typeName": "SSH Username with private key", "description": "Deploy key",
			},
			"store": map[string]any{keyKind: storeKindSystem, "domain": "_"},
			"usages": []map[string]any{
				{
					keyKind: usageKindWorkflow, keyFullName: "my-team/my-service", keyURL: "https://ci.example.com/job/my-team/job/my-service/",
					"parent": map[string]any{keyKind: parentKindFolder, keyFullName: "my-team", keyURL: "https://ci.example.com/job/my-team/"},
				},
				{
					keyKind: usageKindMultibranch, keyFullName: "my-team/my-app", keyURL: "https://ci.example.com/job/my-team/job/my-app/",
					"parent":   map[string]any{keyKind: parentKindFolder, keyFullName: "my-team", keyURL: "https://ci.example.com/job/my-team/"},
					"branches": []string{"main", "PR-7"},
				},
			},
		}, grouped[credentialType][0].Values)
		assert.Equal(t, map[string]any{
			"credential": map[string]any{"id": "team-token", "displayName": "Team token", "typeName": "Secret text", "description": ""},
			"store": map[string]any{
				keyKind: storeKindFolder, "domain": "_",
				"folder": map[string]any{keyKind: containerKindFolder, keyFullName: "my-team", keyURL: "https://ci.example.com/job/my-team/"},
			},
			"usages": []map[string]any{},
		}, grouped[credentialType][1].Values)
	})

	t.Run("plugins", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, map[string]any{
			"plugin": map[string]any{
				"shortName": "credentials", "longName": "Credentials Plugin", "version": "1415.v831096eb_5534",
				"active": true, "enabled": true, "hasUpdate": false, keyURL: "https://plugins.jenkins.io/credentials",
				"requiredCoreVersion": "2.555.1",
				"dependencies":        []map[string]any{{"shortName": "structs", "version": "350.v3b_30f09f2363", "optional": false}},
			},
		}, grouped[pluginType][0].Values)
		assert.Equal(t, "pipeline-stage-view", grouped[pluginType][1].Values["plugin"].(map[string]any)["shortName"])
	})

	t.Run("never reads branches other than the primary one", func(t *testing.T) {
		t.Parallel()
		for _, path := range fake.requested() {
			assert.NotContains(t, path, "feature")
			assert.NotContains(t, path, "PR-7")
			assert.NotContains(t, path, "develop")
		}
	})
}

func TestStartSyncProcessRequestsOnlyWhatIsNeeded(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		types            []string
		expectedTypes    map[string]int
		expectedPaths    []string
		forbiddenSubpath []string
	}{
		"plugins only": {
			types:         []string{pluginType},
			expectedTypes: map[string]int{pluginType: 2},
			expectedPaths: []string{pluginManagerAPIPath},
		},
		"instance only": {
			types:         []string{instanceType},
			expectedTypes: map[string]int{instanceType: 1},
			expectedPaths: []string{rootListingPath},
		},
		"unknown types only": {
			types:         []string{"build", "stage"},
			expectedTypes: map[string]int{},
			expectedPaths: nil,
		},
		"folders only": {
			types:         []string{folderType},
			expectedTypes: map[string]int{folderType: 2},
			expectedPaths: []string{rootListingPath, myTeamListingPath, myOrgListingPath},
		},
		"multibranch only": {
			types:            []string{multibranchType},
			expectedTypes:    map[string]int{multibranchType: 2},
			expectedPaths:    []string{rootListingPath, myTeamListingPath, myAppListingPath, myOrgListingPath, myRepoListingPath},
			forbiddenSubpath: []string{"wfapi", "/job/main/"},
		},
		"shared libraries only": {
			types:            []string{sharedLibraryType},
			expectedTypes:    map[string]int{sharedLibraryType: 2},
			expectedPaths:    []string{rootListingPath, myTeamListingPath, myAppListingPath, myAppMainPath, myOrgListingPath, myRepoListingPath},
			forbiddenSubpath: []string{"wfapi"},
		},
		"credentials only": {
			types:         []string{credentialType},
			expectedTypes: map[string]int{credentialType: 2},
			expectedPaths: []string{
				rootListingPath, myTeamListingPath, myOrgListingPath,
				systemStoreAPIPath, myTeamStoreAPIPath,
				"/job/my-team/job/my-app/credentials/store/folder/api/json",
				"/job/my-org/credentials/store/folder/api/json",
				"/job/my-org/job/my-repo/credentials/store/folder/api/json",
			},
			forbiddenSubpath: []string{"wfapi", "/job/main/"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data, requested, err := runSync(t, fixtureRoutes(), tc.types...)
			require.NoError(t, err)

			counts := make(map[string]int)
			for _, d := range data {
				counts[d.Type]++
			}
			assert.Equal(t, tc.expectedTypes, counts)
			assert.ElementsMatch(t, tc.expectedPaths, requested)
			for _, path := range requested {
				for _, forbidden := range tc.forbiddenSubpath {
					assert.NotContains(t, path, forbidden)
				}
			}
		})
	}
}

func TestStartSyncProcessIsolatesFailures(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		overrides     map[string]fakeResponse
		types         []string
		expectedTypes map[string]int
		expectedError string
	}{
		"root listing fails": {
			overrides:     map[string]fakeResponse{rootListingPath + "?tree=" + containerTree: failingResponse},
			types:         []string{instanceType, folderType, workflowType, credentialType, pluginType},
			expectedTypes: map[string]int{instanceType: 1, pluginType: 2},
			expectedError: "folder tree",
		},
		"plugins forbidden": {
			overrides:     map[string]fakeResponse{pluginManagerAPIPath: {status: http.StatusForbidden}},
			types:         []string{instanceType, folderType, pluginType},
			expectedTypes: map[string]int{instanceType: 1, folderType: 2},
			expectedError: "plugins",
		},
		"instance fails": {
			overrides:     map[string]fakeResponse{rootListingPath + "?tree=" + instanceTree: failingResponse},
			types:         []string{instanceType, folderType},
			expectedTypes: map[string]int{folderType: 2},
			expectedError: "instance",
		},
		"system credentials store fails": {
			overrides:     map[string]fakeResponse{systemStoreAPIPath: failingResponse},
			types:         []string{credentialType, sharedLibraryType},
			expectedTypes: map[string]int{sharedLibraryType: 2},
			expectedError: "credentials",
		},
		"folder credentials store fails": {
			overrides:     map[string]fakeResponse{myTeamStoreAPIPath: failingResponse},
			types:         []string{credentialType},
			expectedTypes: map[string]int{credentialType: 1},
		},
		"folder listing fails": {
			overrides:     map[string]fakeResponse{myTeamListingPath: failingResponse},
			types:         []string{folderType, multibranchType, workflowType},
			expectedTypes: map[string]int{folderType: 2, multibranchType: 1, workflowType: 1},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data, _, err := runSync(t, withRoutes(fixtureRoutes(), tc.overrides), tc.types...)
			if tc.expectedError != "" {
				require.ErrorIs(t, err, ErrJenkinsSource)
				assert.ErrorContains(t, err, tc.expectedError)
			} else {
				require.NoError(t, err)
			}

			counts := make(map[string]int)
			for _, d := range data {
				counts[d.Type]++
			}
			assert.Equal(t, tc.expectedTypes, counts)
		})
	}
}

func TestStartSyncProcessSkipsWhenAlreadyRunning(t *testing.T) {
	t.Parallel()

	fake := newFakeJenkins(fixtureRoutes())
	src := newTestSource(t, fake)
	src.syncLock.Lock()
	defer src.syncLock.Unlock()

	results := make(chan source.Data, 10)
	err := src.StartSyncProcess(t.Context(), map[string]source.MappingExtras{pluginType: {}}, results)
	require.NoError(t, err)
	assert.Empty(t, fake.requested())
	assert.Empty(t, results)
}

func TestStartSyncProcessStopsOnCanceledContext(t *testing.T) {
	t.Parallel()

	fake := newFakeJenkins(fixtureRoutes())
	src := newTestSource(t, fake)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	typesToSync := make(map[string]source.MappingExtras)
	for _, dataType := range allTypes {
		typesToSync[dataType] = source.MappingExtras{}
	}

	results := make(chan source.Data)
	require.NoError(t, src.StartSyncProcess(ctx, typesToSync, results))
}

func TestStartSyncProcessStopsWhenResultsAreNotConsumed(t *testing.T) {
	t.Parallel()

	fake := newFakeJenkins(fixtureRoutes())
	src := newTestSource(t, fake)

	ctx, cancel := context.WithCancel(t.Context())
	results := make(chan source.Data)
	done := make(chan error)
	go func() {
		done <- src.StartSyncProcess(ctx, map[string]source.MappingExtras{folderType: {}, credentialType: {}, pluginType: {}}, results)
	}()

	first := <-results
	assert.Equal(t, folderType, first.Type)
	cancel()

	require.NoError(t, <-done)
}
