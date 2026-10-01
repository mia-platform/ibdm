// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mappingconfig "github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/mapper"
)

// examplePath is the internal mapping shipped in the binary.
const examplePath = "../../mappings/data/sonarqube/issues.yaml"

// TestExampleMapping renders the documented mapping over the values this
// source emits, so the two cannot drift apart silently.
func TestExampleMapping(t *testing.T) {
	t.Parallel()

	mappings, err := mappingconfig.NewMappingConfigsFromPath(examplePath)
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	mapping := mappings[0]
	assert.Equal(t, issueType, mapping.Type)

	m, err := mapper.New(mapping.Mappings.Identifier, mapping.Mappings.Metadata, mapping.Mappings.Spec, mapping.Mappings.Extra)
	require.NoError(t, err)

	t.Run("a complete issue", func(t *testing.T) {
		t.Parallel()

		rules := map[string]map[string]any{"java:S1192": testRules()[0]}
		output, _, err := m.ApplyTemplates(issueValues(testIssue("AZaBcDeFgHiJkLmNoPqR"), rules, testAnalysis(t)), mapper.ParentItemInfo{})
		require.NoError(t, err)

		// The name catalog-sonarqube-webhook gave the same issue, so that a
		// switch-over updates the existing items instead of duplicating them.
		assert.Equal(t, "my-project-2024-f60dda5049896158", output.Identifier)

		assert.Equal(t, "Define a constant instead of duplicating this literal 3 times.", output.Metadata["title"])
		assert.Equal(t, "My-Project_2024", output.Metadata["labels"].(map[string]any)["sonarqube.mia-platform.eu/project"])
		assert.Equal(t, []any{"sonarqube", "design", "bad-practice"}, output.Metadata["tags"])
		links := output.Metadata["links"].([]any)
		require.Len(t, links, 2)
		assert.Contains(t, links[0].(map[string]any)["url"], "&open=AZaBcDeFgHiJkLmNoPqR")

		spec := output.Spec
		assert.Equal(t, "AZaBcDeFgHiJkLmNoPqR", spec["issueKey"])
		assert.Equal(t, "My-Project_2024", spec["projectKey"])
		assert.Equal(t, "feature/new-thing", spec["branch"])
		assert.Nil(t, spec["pullRequest"])
		assert.Equal(t, false, spec["isMainBranch"])
		assert.Equal(t, "AZaTaskId", spec["analysisId"])
		assert.Equal(t, "String literals should not be duplicated", spec["ruleName"])
		assert.Equal(t, "Java", spec["language"])
		assert.Equal(t, "src/main/java/App.java", spec["componentPath"])
		assert.Equal(t, 42, spec["line"])
		assert.Equal(t, map[string]any{"startLine": 42, "endLine": 42, "startOffset": 4, "endOffset": 19}, spec["textRange"])
		assert.Equal(t, "HIGH", spec["severity"])
		assert.Equal(t, "OPEN", spec["issueStatus"])
		assert.Equal(t, "8min", spec["effort"])
		assert.Equal(t, []any{"design", "Bad Practice"}, spec["issueTags"])
		assert.Equal(t, false, spec["quickFixAvailable"])
		assert.Equal(t, "2026-09-15T08:00:00Z", spec["creationDate"])
		assert.Nil(t, spec["closeDate"])
		assert.Len(t, spec["impacts"], 2)
		assert.Equal(t, "https://github.com/org/repo/blob/"+testRevision+"/src/main/java/App.java#L42", spec["scmUrl"])
	})

	t.Run("a minimal issue renders absent fields as null", func(t *testing.T) {
		t.Parallel()

		analysis := analysisContext{serverURL: "https://sonar.example.com", projectKey: "my-project"}
		output, _, err := m.ApplyTemplates(issueValues(map[string]any{"key": "AZaKey"}, nil, analysis), mapper.ParentItemInfo{})
		require.NoError(t, err)

		assert.Regexp(t, `^my-project-[0-9a-f]{16}$`, output.Identifier)
		assert.Equal(t, "AZaKey", output.Metadata["title"])
		for _, field := range []string{"rule", "ruleName", "message", "line", "textRange", "severity", "branch", "scmUrl", "isMainBranch"} {
			assert.Contains(t, output.Spec, field)
			assert.Nil(t, output.Spec[field], field)
		}
		assert.Equal(t, []any{}, output.Spec["impacts"])
		assert.Equal(t, []any{}, output.Spec["issueTags"])
	})

	t.Run("a message carrying yaml syntax stays one string", func(t *testing.T) {
		t.Parallel()

		issue := map[string]any{"key": "AZaKey", "message": `Remove "this": {a: b} # not a comment <script>`}
		analysis := analysisContext{serverURL: "https://sonar.example.com", projectKey: "my-project"}
		output, _, err := m.ApplyTemplates(issueValues(issue, nil, analysis), mapper.ParentItemInfo{})
		require.NoError(t, err)

		assert.Equal(t, issue["message"], output.Spec["message"])
		assert.Equal(t, issue["message"], output.Metadata["title"])
	})
}

// loadExampleMapper builds the mapper of a mapping shipped in the documentation.
func loadExampleMapper(t *testing.T, path, dataType string) mapper.Mapper {
	t.Helper()

	mappings, err := mappingconfig.NewMappingConfigsFromPath(path)
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	mapping := mappings[0]
	require.Equal(t, dataType, mapping.Type)

	m, err := mapper.New(mapping.Mappings.Identifier, mapping.Mappings.Metadata, mapping.Mappings.Spec, mapping.Mappings.Extra)
	require.NoError(t, err)
	return m
}

const runsExamplePath = "../../mappings/data/sonarqube/runs.yaml"

func TestExampleRunMapping(t *testing.T) {
	t.Parallel()

	m := loadExampleMapper(t, runsExamplePath, runType)

	analysis := testAnalysis(t)
	analysis.runKey = runKey(analysis)
	run := runInfo{status: "SUCCESS", conditions: []map[string]any{{"metricKey": "new_coverage", "comparator": "LT", "status": "ERROR", "actualValue": "82.5", "errorThreshold": "85"}}}
	fetched := &analysisIssues{issues: []map[string]any{testIssue("a")}}

	output, extra, err := m.ApplyTemplates(runValues(analysis, run, fetched), mapper.ParentItemInfo{})
	require.NoError(t, err)
	assert.Empty(t, extra)

	assert.Regexp(t, `^my-project-2024-[0-9a-f]{16}$`, output.Identifier)
	assert.Equal(t, "Quality gate ERROR (Sonar way)", output.Metadata["description"])

	spec := output.Spec
	assert.Equal(t, "My-Project_2024", spec["projectKey"])
	assert.Equal(t, "feature/new-thing", spec["branch"])
	assert.Equal(t, "SUCCESS", spec["status"])
	assert.Equal(t, "ERROR", spec["qualityGateStatus"])
	assert.Equal(t, testRevision, spec["revision"])
	assert.Equal(t, 1, spec["issuesRead"])
	assert.Equal(t, false, spec["truncated"])
	assert.Equal(t, map[string]any{"HIGH": 1}, spec["issueCounts"])
	assert.Equal(t, []any{map[string]any{"metric": "new_coverage", "operator": "LESS_THAN", "status": "ERROR", "value": "82.5", "errorThreshold": "85"}}, spec["qualityGateConditions"])
	assert.Nil(t, spec["analysisKey"])
}

func TestExampleIssueToRunRelationship(t *testing.T) {
	t.Parallel()

	issues := loadExampleMapper(t, examplePath, issueType)
	runs := loadExampleMapper(t, runsExamplePath, runType)

	analysis := testAnalysis(t)
	analysis.runKey = runKey(analysis)
	issue := testIssue("AZaBcDeFgHiJkLmNoPqR")

	runOutput, _, err := runs.ApplyTemplates(runValues(analysis, runInfo{status: "SUCCESS"}, nil), mapper.ParentItemInfo{})
	require.NoError(t, err)
	issueOutput, extra, err := issues.ApplyTemplates(issueValues(issue, nil, analysis), mapper.ParentItemInfo{})
	require.NoError(t, err)

	require.Len(t, extra, 1)
	relationship := extra[0]
	assert.Equal(t, "mia-platform.eu/v1", relationship.APIVersion)
	assert.Equal(t, "relationships", relationship.ItemFamily)
	assert.Equal(t, "urn:mia-platform-catalog:sonarqube.mia-platform.eu:v1:Issue:"+issueOutput.Identifier, relationship.Spec["sourceRef"])
	assert.Equal(t, "urn:mia-platform-catalog:sonarqube.mia-platform.eu:v1:Run:"+runOutput.Identifier, relationship.Spec["targetRef"],
		"the relationship points at the run the run mapping writes")
	assert.Equal(t, "urn:mia-platform-catalog:mia-platform.eu:v1:RelationshipType:part-of.mia-platform.eu", relationship.Spec["typeRef"])

	t.Run("a later run moves the relationship instead of adding one", func(t *testing.T) {
		t.Parallel()

		later := analysis
		later.analysedAt = "2026-09-16T08:00:00Z"
		later.runKey = runKey(later)
		_, laterExtra, err := issues.ApplyTemplates(issueValues(issue, nil, later), mapper.ParentItemInfo{})
		require.NoError(t, err)
		require.Len(t, laterExtra, 1)

		assert.Equal(t, relationship.Identifier, laterExtra[0].Identifier)
		assert.NotEqual(t, relationship.Spec["targetRef"], laterExtra[0].Spec["targetRef"])
	})

	t.Run("no run, no relationship", func(t *testing.T) {
		t.Parallel()

		withoutRun := analysis
		withoutRun.runKey = ""
		_, noExtra, err := issues.ApplyTemplates(issueValues(issue, nil, withoutRun), mapper.ParentItemInfo{})
		require.NoError(t, err)
		assert.Empty(t, noExtra)
	})
}
