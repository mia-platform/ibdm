// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

const myServiceBuildURL = "https://ci.example.com/job/my-team/job/my-service/12/"

// syncWorkflows runs a workflow sync against routes and returns the emitted builds by workflow
// full name, with the fake server to inspect the requests.
func syncWorkflows(t *testing.T, routes map[string]fakeResponse) (map[string]map[string]any, *fakeJenkins) {
	t.Helper()

	fake := newFakeJenkins(routes)
	src := newTestSource(t, fake)
	results := make(chan source.Data, 100)
	require.NoError(t, src.StartSyncProcess(t.Context(), map[string]source.MappingExtras{workflowType: {}}, results))
	close(results)

	builds := make(map[string]map[string]any)
	for _, d := range collectData(t, results) {
		workflow := d.Values["workflow"].(map[string]any)
		if build, ok := d.Values["build"].(map[string]any); ok {
			builds[workflow[keyFullName].(string)] = build
		}
	}
	return builds, fake
}

// countRequests returns how many requested URIs contain fragment.
func countRequests(fake *fakeJenkins, fragment string) int {
	count := 0
	for _, uri := range fake.requestedURIs() {
		if strings.Contains(uri, fragment) {
			count++
		}
	}
	return count
}

// myServiceGraphStages returns the stages expected from the Graph View fixture, with failure as
// the failure details of the Run stage.
func myServiceGraphStages(failure map[string]any) []map[string]any {
	stage := func(id, name, status string, start, duration int64, agent string, parallel, synthetic bool, children ...string) map[string]any {
		if children == nil {
			children = []string{}
		}
		return map[string]any{
			"id": id, keyName: name, "status": status, "startTimeMillis": start, "durationMillis": duration,
			"agent": agent, "parallel": parallel, "synthetic": synthetic, "children": children,
		}
	}

	run := stage("23", "Run", "failure", 1767261634000, 56000, "agent-3", false, false)
	run["failure"] = failure

	return []map[string]any{
		stage("6", "Build", "success", 1767261601000, 30000, "agent-1", false, false),
		stage("15", "Test", "failure", 1767261631000, 60000, "", false, false, "18", "19"),
		stage("18", "Unit", "success", 1767261632000, 20000, "agent-2", true, false),
		stage("19", "Integration", "failure", 1767261632000, 58000, "agent-3", true, false, "22", "23"),
		stage("22", "Setup", "success", 1767261633000, 1000, "agent-3", false, false),
		run,
		stage("25", "Deploy", "skipped", 1767261691000, 0, "", false, false),
		stage("30", "Declarative: Post Actions", "success", 1767261692000, 2000, "", false, true),
	}
}

func TestGraphViewStages(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		installed       []string
		expectedFailure map[string]any
		exceptionText   bool
	}{
		"graph view only: error parsed from the exception text": {
			installed: []string{graphViewPluginName},
			expectedFailure: map[string]any{
				"message":    "script returned exit code 2",
				"type":       "hudson.AbortException",
				"nodeName":   "make integration",
				"logUrl":     myServiceBuildURL + "execution/node/24/log/",
				"consoleUrl": myServiceBuildURL + "console",
			},
			exceptionText: true,
		},
		"graph view and stage view: error read from stage view": {
			installed: []string{graphViewPluginName, stageViewPluginName},
			expectedFailure: map[string]any{
				"message":    "script returned exit code 2",
				"type":       "hudson.AbortException",
				"nodeName":   "make integration",
				"logUrl":     myServiceBuildURL + "execution/node/24/log/",
				"consoleUrl": myServiceBuildURL + "console",
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			builds, fake := syncWorkflows(t, graphViewRoutes(tc.installed...))

			build := builds["my-team/my-service"]
			assert.Equal(t, graphViewPluginName, build["stagesSource"])
			assert.Equal(t, myServiceGraphStages(tc.expectedFailure), build["stages"])

			mainBuild := builds["my-team/my-app/main"]
			assert.Equal(t, graphViewPluginName, mainBuild["stagesSource"])
			assert.Len(t, mainBuild["stages"], 1)

			assert.Zero(t, countRequests(fake, "/wfapi/describe?"))
			assert.Zero(t, countRequests(fake, "12/wfapi/describe"))
			if tc.exceptionText {
				assert.Equal(t, 1, countRequests(fake, "/stages/exceptionText?nodeId=24"))
				assert.Zero(t, countRequests(fake, "/execution/node/"))
			} else {
				assert.Zero(t, countRequests(fake, "/stages/exceptionText"))
				assert.Equal(t, 1, countRequests(fake, "/execution/node/24/wfapi/describe"))
			}
		})
	}
}

func TestStagePluginProbing(t *testing.T) {
	t.Parallel()

	forbidden := fakeResponse{status: http.StatusForbidden, body: "forbidden"}
	pluginListForbidden := map[string]fakeResponse{
		pluginManagerAPIPath:                              forbidden,
		pluginManagerAPIPath + "?tree=" + pluginStateTree: forbidden,
	}

	t.Run("graph view answers: used for every workflow", func(t *testing.T) {
		t.Parallel()

		builds, fake := syncWorkflows(t, withRoutes(graphViewRoutes(), pluginListForbidden))
		assert.Equal(t, graphViewPluginName, builds["my-team/my-service"]["stagesSource"])
		assert.Equal(t, graphViewPluginName, builds["my-team/my-app/main"]["stagesSource"])
		assert.Equal(t, 2, countRequests(fake, "/stages/tree"))
		assert.Zero(t, countRequests(fake, "12/wfapi/describe"))
		assert.Equal(t, 1, countRequests(fake, "/execution/node/24/wfapi/describe"), "stage view probed for the error")
	})

	t.Run("graph view missing: probed once, then stage view", func(t *testing.T) {
		t.Parallel()

		builds, fake := syncWorkflows(t, withRoutes(fixtureRoutes(), pluginListForbidden))
		assert.Equal(t, stageViewPluginName, builds["my-team/my-service"]["stagesSource"])
		assert.Equal(t, stageViewPluginName, builds["my-team/my-app/main"]["stagesSource"])
		assert.Equal(t, 1, countRequests(fake, "/stages/tree"))
	})

	t.Run("no stage plugin: probed once each, workflows without stages", func(t *testing.T) {
		t.Parallel()

		routes := withRoutes(fixtureRoutes(), pluginListForbidden)
		delete(routes, myServiceRunPath)
		delete(routes, myAppMainRunPath)

		builds, fake := syncWorkflows(t, routes)
		assert.NotContains(t, builds["my-team/my-service"], "stages")
		assert.NotContains(t, builds["my-team/my-app/main"], "stagesSource")
		assert.Equal(t, 1, countRequests(fake, "/stages/tree"))
		assert.Equal(t, 1, countRequests(fake, "/wfapi/describe"))
	})

	t.Run("plugin list without stage plugins: never called", func(t *testing.T) {
		t.Parallel()

		builds, fake := syncWorkflows(t, graphViewRoutes("credentials"))
		assert.NotContains(t, builds["my-team/my-service"], "stages")
		assert.Zero(t, countRequests(fake, "/stages/"))
		assert.Zero(t, countRequests(fake, "/wfapi/"))
	})
}

func TestGraphViewFallbacks(t *testing.T) {
	t.Parallel()

	failureWithStep := map[string]any{
		"nodeName":   "make integration",
		"logUrl":     myServiceBuildURL + "execution/node/24/log/",
		"consoleUrl": myServiceBuildURL + "console",
	}

	testCases := map[string]struct {
		installed        []string
		overrides        map[string]fakeResponse
		expectedSource   string
		expectedFailure  map[string]any
		expectedRequests []string
	}{
		"tree with unexpected status falls back to stage view": {
			installed:      []string{graphViewPluginName, stageViewPluginName},
			overrides:      map[string]fakeResponse{myServiceGraphTreePath: {body: `{"status": "error", "data": {"error": "boom"}}`}},
			expectedSource: stageViewPluginName,
		},
		"tree failing falls back to stage view": {
			installed:      []string{graphViewPluginName, stageViewPluginName},
			overrides:      map[string]fakeResponse{myServiceGraphTreePath: failingResponse},
			expectedSource: stageViewPluginName,
		},
		"steps not readable: exception text of the stage": {
			installed:        []string{graphViewPluginName},
			overrides:        map[string]fakeResponse{myServiceGraphStepsPath: failingResponse},
			expectedSource:   graphViewPluginName,
			expectedFailure:  map[string]any{"message": "script returned exit code 2", "type": "hudson.AbortException", "consoleUrl": myServiceBuildURL + "console"},
			expectedRequests: []string{"/stages/exceptionText?nodeId=23"},
		},
		"exception text not readable": {
			installed:       []string{graphViewPluginName},
			overrides:       map[string]fakeResponse{myServiceGraphExceptionPath: failingResponse},
			expectedSource:  graphViewPluginName,
			expectedFailure: failureWithStep,
		},
		"stage view error not readable: exception text": {
			installed: []string{graphViewPluginName, stageViewPluginName},
			overrides: map[string]fakeResponse{myServiceStepDescribePath: failingResponse},
			expectedFailure: map[string]any{
				"message": "script returned exit code 2", "type": "hudson.AbortException",
				"nodeName": "make integration", "logUrl": myServiceBuildURL + "execution/node/24/log/", "consoleUrl": myServiceBuildURL + "console",
			},
			expectedSource:   graphViewPluginName,
			expectedRequests: []string{"/execution/node/24/wfapi/describe", "/stages/exceptionText?nodeId=24"},
		},
		"stage view error from the failed step of the stage": {
			installed: []string{graphViewPluginName, stageViewPluginName},
			overrides: map[string]fakeResponse{
				myServiceGraphStepsPath: {body: `{"status": "ok", "data": {"steps": []}}`},
				"/job/my-team/job/my-service/12/execution/node/23/wfapi/describe": {body: `{"id": "23", "name": "Run",
					"stageFlowNodes": [{"id": "24", "name": "Shell Script", "error": {"message": "script returned exit code 2", "type": "hudson.AbortException"}}]}`},
			},
			expectedSource: graphViewPluginName,
			expectedFailure: map[string]any{
				"message": "script returned exit code 2", "type": "hudson.AbortException",
				"nodeName": "Shell Script", "logUrl": myServiceBuildURL + "execution/node/24/log/", "consoleUrl": myServiceBuildURL + "console",
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			builds, fake := syncWorkflows(t, withRoutes(graphViewRoutes(tc.installed...), tc.overrides))
			build := builds["my-team/my-service"]
			assert.Equal(t, tc.expectedSource, build["stagesSource"])

			if tc.expectedFailure != nil {
				stages := build["stages"].([]map[string]any)
				idx := slices.IndexFunc(stages, func(stage map[string]any) bool { return stage["id"] == "23" })
				require.GreaterOrEqual(t, idx, 0)
				assert.Equal(t, tc.expectedFailure, stages[idx]["failure"])
			}
			for _, fragment := range tc.expectedRequests {
				assert.Equal(t, 1, countRequests(fake, fragment), fragment)
			}
		})
	}
}

func TestGraphViewUnhandledException(t *testing.T) {
	t.Parallel()

	unhandled := `{"id": "26", "name": "Pipeline error", "state": "failure", "type": "UNHANDLED_EXCEPTION", "title": "",
		"pauseDurationMillis": 0, "startTimeMillis": 1767261690000, "stageId": "23", "flags": {}}`
	failedStep := `{"id": "24", "name": "make integration", "state": "failure", "type": "STEP", "title": "",
		"pauseDurationMillis": 0, "startTimeMillis": 1767261635000, "stageId": "23", "flags": {}}`

	testCases := map[string]struct {
		steps           string
		expectedFailure map[string]any
	}{
		"only an error outside any step": {
			steps: unhandled,
			expectedFailure: map[string]any{
				"message": "script returned exit code 2", "type": "hudson.AbortException",
				"nodeName": "Pipeline error", "consoleUrl": myServiceBuildURL + "console",
			},
		},
		"a failed step is preferred": {
			steps: unhandled + ", " + failedStep,
			expectedFailure: map[string]any{
				"message": "script returned exit code 2", "type": "hudson.AbortException",
				"nodeName": "make integration", "logUrl": myServiceBuildURL + "execution/node/24/log/",
				"consoleUrl": myServiceBuildURL + "console",
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			routes := withRoutes(graphViewRoutes(graphViewPluginName), map[string]fakeResponse{
				myServiceGraphStepsPath: {body: `{"status": "ok", "data": {"runIsComplete": true, "steps": [` + tc.steps + `]}}`},
			})
			builds, _ := syncWorkflows(t, routes)
			stages := builds["my-team/my-service"]["stages"].([]map[string]any)
			idx := slices.IndexFunc(stages, func(stage map[string]any) bool { return stage["id"] == "23" })
			require.GreaterOrEqual(t, idx, 0)
			assert.Equal(t, tc.expectedFailure, stages[idx]["failure"])
		})
	}
}

func TestFlattenGraphStagesSkipsRepeatedAndCyclicReferences(t *testing.T) {
	t.Parallel()

	second := &graphStage{ID: "2", Name: "Second", State: "success"}
	first := &graphStage{ID: "1", Name: "First", State: "success", NextSibling: second}
	second.NextSibling = first

	stages := []graphStage{
		{ID: "0", Name: "Parent", State: "success", Children: []graphStage{*first, *second}},
		*second,
		{Name: "Without id"},
	}

	entries := flattenGraphStages(stages)
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.id)
	}
	assert.Equal(t, []string{"0", "1", "2"}, ids)
	assert.Equal(t, []string{"1", "2"}, entries[0].children)
}

func TestGraphViewInvalidTreeFallsBack(t *testing.T) {
	t.Parallel()

	routes := withRoutes(graphViewRoutes(graphViewPluginName, stageViewPluginName), map[string]fakeResponse{
		myServiceGraphTreePath: {body: `{"status": "ok", "data": {"stages": [{"id": "15", "children": ["18"]}]}}`},
	})

	builds, _ := syncWorkflows(t, routes)
	assert.Equal(t, stageViewPluginName, builds["my-team/my-service"]["stagesSource"])
}

func TestParseExceptionText(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		text            string
		expectedMessage string
		expectedType    string
	}{
		"exception with message": {
			text:            "hudson.AbortException: script returned exit code 1",
			expectedMessage: "script returned exit code 1",
			expectedType:    "hudson.AbortException",
		},
		"exception with stack trace": {
			text:            "hudson.AbortException: Deploy failed\r\n\tat org.jenkinsci.plugins.workflow.steps.ErrorStep$Execution.run(ErrorStep.java:64)\r\n",
			expectedMessage: "Deploy failed",
			expectedType:    "hudson.AbortException",
		},
		"exception without message": {
			text:         "org.jenkinsci.plugins.workflow.steps.FlowInterruptedException\n    at org.example.Step.run(Step.java:1)",
			expectedType: "org.jenkinsci.plugins.workflow.steps.FlowInterruptedException",
		},
		"inner class": {
			text:            "org.example.Outer$InnerException: broken",
			expectedMessage: "broken",
			expectedType:    "org.example.Outer$InnerException",
		},
		"message holding colons": {
			text:            "java.io.IOException: Connection reset: by peer",
			expectedMessage: "Connection reset: by peer",
			expectedType:    "java.io.IOException",
		},
		"multi-line message": {
			text:            "hudson.AbortException: first line\nsecond line\n\tat org.example.Step.run(Step.java:1)",
			expectedMessage: "first line\nsecond line",
			expectedType:    "hudson.AbortException",
		},
		"plain text": {
			text:            "Timeout has been exceeded",
			expectedMessage: "Timeout has been exceeded",
		},
		"file name is not a class": {
			text:            "config.yaml: not found",
			expectedMessage: "config.yaml: not found",
		},
		"empty": {},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			message, errType := parseExceptionText(tc.text)
			assert.Equal(t, tc.expectedMessage, message)
			assert.Equal(t, tc.expectedType, errType)
		})
	}
}

func TestGraphViewStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, stageStatusFailure, graphViewStatus("failure"))
	assert.Equal(t, "not_built", graphViewStatus("NOT_BUILT"))
	assert.Equal(t, stageStatusUnknown, graphViewStatus(""))
}

func TestStepNameFallsBackToArguments(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Git", stepName(&graphStep{Name: "https://git.example.com/my-org/my-app.git", Title: "Git"}))
	assert.Equal(t, "make test", stepName(&graphStep{Name: "make test"}))
}
