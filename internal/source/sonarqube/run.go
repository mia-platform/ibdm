// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"net/url"
	"strings"
)

// runType is the data type of an analysis run.
const runType = "run"

// conditionOperators maps the operator spelling of /api/qualitygates/project_status
// to the one of the webhook payload, so that both produce the same conditions.
var conditionOperators = map[string]string{
	"LT": "LESS_THAN",
	"GT": "GREATER_THAN",
	"EQ": "EQUALS",
	"NE": "NOT_EQUALS",
}

// runInfo is what is known about a run beyond its analysis context.
type runInfo struct {
	// status is the compute-engine task status: SUCCESS, FAILED or CANCELED.
	status string
	// analysisKey is the SonarQube analysis key, known to sync only.
	analysisKey    string
	projectVersion string
	// conditions are the quality gate conditions, as the webhook or
	// /api/qualitygates/project_status report them.
	conditions []map[string]any
}

// runKey identifies the run of an analysis: the project, the ref and the
// analysis date. The date is what both a webhook delivery (analysedAt) and
// /api/project_analyses/search (date) report for the same analysis, so a
// webhook and a sync write the same run. The main branch has an empty ref,
// because a sync knows it only as the main branch. The task id is the
// fallback for a delivery without an analysis date.
func runKey(analysis analysisContext) string {
	ref := ""
	switch {
	case analysis.pullRequest != "":
		ref = "pullRequest:" + analysis.pullRequest
	case analysis.branch != "" && (analysis.isMainBranch == nil || !*analysis.isMainBranch):
		ref = "branch:" + analysis.branch
	}

	moment := analysis.analysedAt
	if moment == "" {
		if analysis.taskID == "" {
			return ""
		}
		moment = "task:" + analysis.taskID
	}

	return analysis.projectKey + "|" + ref + "|" + moment
}

// runValues builds the mapping values of one run:
//
//   - analysis: the analysis the run is (same shape as for an issue)
//   - run: the task status, the quality gate conditions and what was read
//   - derived: values computed here because a template cannot
//
// fetched is nil when the issues were not read, because the analysis did not
// succeed or issues are not mapped: the counts are then nil, not zero.
func runValues(analysis analysisContext, run runInfo, fetched *analysisIssues) map[string]any {
	conditions := make([]any, 0, len(run.conditions))
	for _, condition := range run.conditions {
		conditions = append(conditions, normaliseCondition(condition))
	}

	var issuesRead, truncated, issueCounts any
	if fetched != nil {
		issuesRead = len(fetched.issues)
		truncated = fetched.truncated
		issueCounts = countBySeverity(fetched.issues)
	}

	return map[string]any{
		"analysis": analysis.toValues(),
		"run": map[string]any{
			"status":                optional(run.status),
			"analysisKey":           optional(run.analysisKey),
			"projectVersion":        optional(run.projectVersion),
			"qualityGateConditions": conditions,
			"issuesRead":            issuesRead,
			"truncated":             truncated,
			"issueCounts":           issueCounts,
		},
		"derived": runDerivedValues(analysis, run),
	}
}

// runDerivedValues computes the metadata of a run. Unlike an issue, nothing in
// it changes after the run happened, so labels can carry its outcome.
func runDerivedValues(analysis analysisContext, run runInfo) map[string]any {
	var repositoryURL string
	if analysis.scm != nil {
		repositoryURL = analysis.scm.repositoryURL
	}

	dashboardURL := sonarqubeDashboardURL(analysis)
	links := make([]any, 0, 1)
	if dashboardURL != "" {
		links = append(links, map[string]any{"title": sonarqubeLinkTitle, "url": dashboardURL})
	}

	labels := make(map[string]any)
	setLabel(labels, "project", analysis.projectKey)
	setLabel(labels, "branch", analysis.branch)
	setLabel(labels, "pull-request", analysis.pullRequest)
	setLabel(labels, "status", run.status)
	setLabel(labels, "quality-gate", analysis.qualityGateStatus)

	return map[string]any{
		"projectSlug":   projectSlug(analysis.projectKey),
		"title":         runTitle(analysis),
		"description":   runDescription(analysis, run),
		"sonarqubeUrl":  optional(dashboardURL),
		"repositoryUrl": optional(repositoryURL),
		"labels":        labels,
		"tags":          []any{baseTag},
		"links":         links,
	}
}

// runTitle names a run by project, ref and date.
func runTitle(analysis analysisContext) string {
	project := analysis.projectName
	if project == "" {
		project = analysis.projectKey
	}

	ref := "main branch"
	switch {
	case analysis.pullRequest != "":
		ref = "pull request " + analysis.pullRequest
	case analysis.branch != "":
		ref = analysis.branch
	}

	moment := analysis.analysedAt
	if moment == "" {
		moment = analysis.taskID
	}

	parts := []string{project, ref}
	if moment != "" {
		parts = append(parts, moment)
	}
	return truncateRunes(strings.Join(parts, " · "), titleLimit)
}

// runDescription states the outcome of a run in one line.
func runDescription(analysis analysisContext, run runInfo) string {
	if run.status != "" && !strings.EqualFold(run.status, statusSuccess) {
		return "Analysis " + run.status
	}
	if analysis.qualityGateStatus == "" {
		return "Analysis " + statusSuccess
	}
	if analysis.qualityGateName == "" {
		return "Quality gate " + analysis.qualityGateStatus
	}
	return "Quality gate " + analysis.qualityGateStatus + " (" + analysis.qualityGateName + ")"
}

// sonarqubeDashboardURL links to the analysed ref in SonarQube, or returns ""
// when the server URL is not one a browser should follow.
func sonarqubeDashboardURL(analysis analysisContext) string {
	base, err := url.Parse(analysis.serverURL)
	if err != nil || !isHTTPURL(base) {
		return ""
	}

	query := url.Values{}
	query.Set("id", analysis.projectKey)
	switch {
	case analysis.pullRequest != "":
		query.Set("pullRequest", analysis.pullRequest)
	case analysis.branch != "":
		query.Set("branch", analysis.branch)
	}

	link := base.JoinPath("dashboard")
	link.RawQuery = query.Encode()
	link.Fragment = ""
	return link.String()
}

// normaliseCondition reduces a quality gate condition to one shape, whichever
// API reported it: the webhook sends metric/operator/value, project_status
// sends metricKey/comparator/actualValue.
func normaliseCondition(raw map[string]any) map[string]any {
	operator := firstString(raw, "operator", "comparator")
	if long, found := conditionOperators[operator]; found {
		operator = long
	}

	return map[string]any{
		"metric":         optional(firstString(raw, "metric", "metricKey")),
		"operator":       optional(operator),
		"status":         optional(stringField(raw, "status")),
		"value":          optional(firstString(raw, "value", "actualValue")),
		"errorThreshold": optional(stringField(raw, "errorThreshold")),
	}
}

// countBySeverity counts issues by effective severity.
func countBySeverity(issues []map[string]any) map[string]any {
	counts := make(map[string]any)
	for _, issue := range issues {
		severity := effectiveSeverity(issue)
		if severity == "" {
			continue
		}
		count, _ := counts[severity].(int)
		counts[severity] = count + 1
	}
	return counts
}

// firstString returns the first of keys that holds a non-empty string.
func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringField(object, key); value != "" {
			return value
		}
	}
	return ""
}
