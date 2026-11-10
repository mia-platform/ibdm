// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// myServiceBuild returns the build values emitted for the my-team/my-service workflow.
func myServiceBuild(t *testing.T, routes map[string]fakeResponse) map[string]any {
	t.Helper()

	data, _, err := runSync(t, routes, workflowType)
	require.NoError(t, err)
	for _, d := range data {
		workflow := d.Values["workflow"].(map[string]any)
		if workflow[keyFullName] == "my-team/my-service" {
			return d.Values["build"].(map[string]any)
		}
	}
	require.FailNow(t, "workflow my-team/my-service not emitted")
	return nil
}

func TestStagesUnavailable(t *testing.T) {
	t.Parallel()

	testCases := map[string]fakeResponse{
		"stage view not installed": {status: http.StatusNotFound, body: "missing"},
		"server error":             failingResponse,
		"invalid response":         {body: `{"stages": "nope"}`},
	}

	for name, response := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			routes := withRoutes(fixtureRoutes(), map[string]fakeResponse{myServiceRunPath: response})
			build := myServiceBuild(t, routes)
			assert.NotContains(t, build, "stages")
			assert.Equal(t, int64(12), build["number"])
		})
	}
}

func TestStageFailureFallsBackToStageError(t *testing.T) {
	t.Parallel()

	testCases := map[string]fakeResponse{
		"node description fails":     failingResponse,
		"no flow node with an error": {body: `{"stageFlowNodes": [{"id": "16", "name": "Print Message"}]}`},
	}

	for name, response := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			routes := withRoutes(fixtureRoutes(), map[string]fakeResponse{myServiceTestPath: response})
			build := myServiceBuild(t, routes)
			stages := build["stages"].([]map[string]any)
			assert.Equal(t, map[string]any{
				"message":    "script returned exit code 1",
				"type":       "hudson.AbortException",
				"consoleUrl": "https://ci.example.com/job/my-team/job/my-service/12/console",
			}, stages[1]["failure"])
		})
	}
}

func TestStageFailureWithoutAnyError(t *testing.T) {
	t.Parallel()

	routes := withRoutes(fixtureRoutes(), map[string]fakeResponse{
		myServiceRunPath:  {body: `{"stages": [{"id": "15", "name": "Test", "status": "FAILED"}]}`},
		myServiceTestPath: {body: `{"stageFlowNodes": []}`},
	})
	build := myServiceBuild(t, routes)
	stages := build["stages"].([]map[string]any)
	require.Len(t, stages, 1)
	assert.Equal(t, map[string]any{
		"consoleUrl": "https://ci.example.com/job/my-team/job/my-service/12/console",
	}, stages[0]["failure"])
}

func TestStageFailureMessageIsTruncated(t *testing.T) {
	t.Parallel()

	longMessage := strings.Repeat("è", maxErrorMessageLength+10)
	routes := withRoutes(fixtureRoutes(), map[string]fakeResponse{
		myServiceTestPath: {body: `{"stageFlowNodes": [{"id": "17", "name": "Shell Script", "error": {"message": "` + longMessage + `", "type": "hudson.AbortException"}}]}`},
	})
	build := myServiceBuild(t, routes)
	failure := build["stages"].([]map[string]any)[1]["failure"].(map[string]any)
	assert.Equal(t, strings.Repeat("è", maxErrorMessageLength)+truncationSuffix, failure["message"])
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "short", truncate("short", 10))
	assert.Equal(t, "exact", truncate("exact", 5))
	assert.Equal(t, "ab"+truncationSuffix, truncate("abcdef", 2))
}

func TestWithTrailingSlash(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "https://ci.example.com/job/a/1/", withTrailingSlash("https://ci.example.com/job/a/1"))
	assert.Equal(t, "https://ci.example.com/job/a/1/", withTrailingSlash("https://ci.example.com/job/a/1/"))
}

func TestStageViewStatus(t *testing.T) {
	t.Parallel()

	testCases := map[string]string{
		"SUCCESS":              stageStatusSuccess,
		"FAILED":               stageStatusFailure,
		"UNSTABLE":             stageStatusUnstable,
		"ABORTED":              stageStatusAborted,
		"NOT_EXECUTED":         stageStatusSkipped,
		"IN_PROGRESS":          stageStatusRunning,
		"PAUSED_PENDING_INPUT": stageStatusPaused,
		"SOMETHING_NEW":        stageStatusUnknown,
	}

	for status, expected := range testCases {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, expected, stageViewStatus(status))
		})
	}
}
