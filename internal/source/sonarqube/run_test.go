// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunKey(t *testing.T) {
	t.Parallel()

	isMain, notMain := true, false
	base := analysisContext{projectKey: "my-project", analysedAt: "2026-09-15T08:00:00Z", taskID: "AZaTask"}

	t.Run("a webhook and a sync of the same main branch analysis agree", func(t *testing.T) {
		t.Parallel()

		webhook := base
		webhook.branch, webhook.isMainBranch = "main", &isMain
		sync := analysisContext{projectKey: "my-project", analysedAt: "2026-09-15T08:00:00Z", branch: "main", isMainBranch: &isMain}
		noBranch := base

		assert.Equal(t, "my-project||2026-09-15T08:00:00Z", runKey(webhook))
		assert.Equal(t, runKey(webhook), runKey(sync))
		assert.Equal(t, runKey(webhook), runKey(noBranch), "a payload naming no branch is about the main branch")
	})

	t.Run("refs are told apart", func(t *testing.T) {
		t.Parallel()

		branch := base
		branch.branch, branch.isMainBranch = "feature", &notMain
		pullRequest := base
		pullRequest.pullRequest = "42"

		assert.Equal(t, "my-project|branch:feature|2026-09-15T08:00:00Z", runKey(branch))
		assert.Equal(t, "my-project|pullRequest:42|2026-09-15T08:00:00Z", runKey(pullRequest))
	})

	t.Run("the task id is the fallback for a delivery without a date", func(t *testing.T) {
		t.Parallel()

		withoutDate := base
		withoutDate.analysedAt = ""
		assert.Equal(t, "my-project||task:AZaTask", runKey(withoutDate))

		withoutDate.taskID = ""
		assert.Empty(t, runKey(withoutDate), "nothing identifies the run")
	})
}

func TestNormaliseCondition(t *testing.T) {
	t.Parallel()

	expected := map[string]any{"metric": "new_coverage", "operator": "LESS_THAN", "status": "ERROR", "value": "82.5", "errorThreshold": "85"}

	webhook := map[string]any{"metric": "new_coverage", "operator": "LESS_THAN", "status": "ERROR", "value": "82.5", "errorThreshold": "85", "onLeakPeriod": true}
	projectStatus := map[string]any{"metricKey": "new_coverage", "comparator": "LT", "status": "ERROR", "actualValue": "82.5", "errorThreshold": "85", "periodIndex": float64(1)}

	assert.Equal(t, expected, normaliseCondition(webhook))
	assert.Equal(t, expected, normaliseCondition(projectStatus))
	assert.Nil(t, normaliseCondition(map[string]any{"metric": "coverage", "status": "NO_VALUE"})["value"])
}

func TestRunValues(t *testing.T) {
	t.Parallel()

	analysis := testAnalysis(t)
	analysis.runKey = runKey(analysis)
	fetched := &analysisIssues{
		issues: []map[string]any{
			testIssue("a"),
			testIssue("b"),
			{"key": "c", "severity": "MINOR"},
			{"key": "d"},
		},
	}
	run := runInfo{status: "SUCCESS", conditions: []map[string]any{{"metric": "new_coverage", "operator": "LESS_THAN", "status": "ERROR"}}}

	values := runValues(analysis, run, fetched)

	assert.Equal(t, analysis.runKey, values["analysis"].(map[string]any)["runKey"])

	runData := values["run"].(map[string]any)
	assert.Equal(t, "SUCCESS", runData["status"])
	assert.Equal(t, 4, runData["issuesRead"])
	assert.Equal(t, false, runData["truncated"])
	assert.Equal(t, map[string]any{"HIGH": 2, "MINOR": 1}, runData["issueCounts"], "counted by effective severity")
	assert.Len(t, runData["qualityGateConditions"], 1)

	derived := values["derived"].(map[string]any)
	assert.Equal(t, "My Project · feature/new-thing · 2026-09-15T08:00:00Z", derived["title"])
	assert.Equal(t, "Quality gate ERROR (Sonar way)", derived["description"])
	assert.Equal(t, "https://sonar.example.com/dashboard?branch=feature%2Fnew-thing&id=My-Project_2024", derived["sonarqubeUrl"])
	assert.Equal(t, "https://github.com/org/repo", derived["repositoryUrl"])
	assert.Equal(t, map[string]any{
		"sonarqube.mia-platform.eu/project":      "My-Project_2024",
		"sonarqube.mia-platform.eu/branch":       "feature-new-thing",
		"sonarqube.mia-platform.eu/status":       "SUCCESS",
		"sonarqube.mia-platform.eu/quality-gate": "ERROR",
	}, derived["labels"])
}

func TestRunValuesWithoutIssues(t *testing.T) {
	t.Parallel()

	analysis := analysisContext{serverURL: "https://sonar.example.com", projectKey: "my-project", taskID: "AZaTask"}
	values := runValues(analysis, runInfo{status: "FAILED"}, nil)

	runData := values["run"].(map[string]any)
	assert.Nil(t, runData["issuesRead"], "not read is not zero")
	assert.Nil(t, runData["issueCounts"])
	assert.Equal(t, []any{}, runData["qualityGateConditions"])

	derived := values["derived"].(map[string]any)
	assert.Equal(t, "Analysis FAILED", derived["description"])
	assert.Equal(t, "my-project · main branch · AZaTask", derived["title"])
}
