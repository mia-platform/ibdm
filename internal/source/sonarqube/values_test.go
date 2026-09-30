// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testAnalysis(t *testing.T) analysisContext {
	t.Helper()

	scm, _ := newSCMTarget("https://github.com/org/repo", testRevision, "feature/new-thing", scmSettings{provider: providerAuto})
	require.NotNil(t, scm)
	isMain := false

	return analysisContext{
		serverURL:         "https://sonar.example.com",
		projectKey:        "My-Project_2024",
		projectName:       "My Project",
		branch:            "feature/new-thing",
		isMainBranch:      &isMain,
		taskID:            "AZaTaskId",
		analysedAt:        "2026-09-15T08:00:00Z",
		revision:          testRevision,
		qualityGateStatus: "ERROR",
		qualityGateName:   "Sonar way",
		scm:               scm,
	}
}

func TestProjectSlug(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "my-project-2024", projectSlug("My-Project_2024"))
	assert.Equal(t, "org-acme-my-service", projectSlug("org.acme:my-service"))
	assert.Equal(t, projectSlugFallback, projectSlug("///"))
	assert.Len(t, projectSlug("a-very-long-project-key-that-goes-well-past-forty-characters"), projectSlugLimit)
	// Only ASCII is lowercased, exactly as catalog-sonarqube-webhook did: the
	// Kelvin sign would otherwise become a "k".
	assert.Equal(t, "a-b", projectSlug("AKB"))
}

func TestEffectiveSeverityAndStatus(t *testing.T) {
	t.Parallel()

	issue := testIssue("AZaBcDeFgHiJkLmNoPqR")
	assert.Equal(t, "HIGH", effectiveSeverity(issue), "the worst impact wins")
	assert.Equal(t, "OPEN", effectiveStatus(issue))

	legacy := map[string]any{"severity": "CRITICAL", "status": "REOPENED"}
	assert.Equal(t, "CRITICAL", effectiveSeverity(legacy), "the legacy severity is the fallback")
	assert.Equal(t, "REOPENED", effectiveStatus(legacy))
}

func TestComponentPath(t *testing.T) {
	t.Parallel()

	component := func(key string) map[string]any { return map[string]any{"component": key} }

	assert.Equal(t, "src/App.java", componentPath(component("my-project:src/App.java"), "my-project"))
	// A Maven project key carries a colon of its own.
	assert.Equal(t, "src/App.java", componentPath(component("org.acme:my-service:src/App.java"), "org.acme:my-service"))
	assert.Empty(t, componentPath(component("org.acme:my-service"), "org.acme:my-service"), "the project itself has no path")
	assert.Equal(t, "src/App.java", componentPath(component("other:src/App.java"), "my-project"), "the first colon is the fallback")
	assert.Empty(t, componentPath(component("my-project"), ""))
}

func TestRFC3339(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2026-09-15T08:00:00Z", rfc3339("2026-09-15T10:00:00+0200"))
	assert.Equal(t, "2026-09-15T08:00:00Z", rfc3339("2026-09-15T10:00:00+02:00"))
	assert.Equal(t, "2026-09-15T08:00:00Z", rfc3339("2026-09-15T08:00:00.123Z"))
	assert.Empty(t, rfc3339("yesterday"))
	assert.Empty(t, rfc3339(""))
}

func TestSanitisers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "feature-new-thing", sanitiseLabelValue("feature/new-thing"))
	assert.Equal(t, "org.acme-my-service", sanitiseLabelValue("org.acme:my-service"))
	assert.Empty(t, sanitiseLabelValue("///"))
	assert.Len(t, sanitiseLabelValue(strings.Repeat("a", 100)), labelValueLimit)

	assert.Equal(t, "bad-practice", sanitiseTag("Bad Practice"))
	assert.Equal(t, "c++", sanitiseTag("C++"))
	assert.Empty(t, sanitiseTag("///"))
}

func TestIssueValues(t *testing.T) {
	t.Parallel()

	rules := map[string]map[string]any{"java:S1192": testRules()[0]}
	values := issueValues(testIssue("AZaBcDeFgHiJkLmNoPqR"), rules, testAnalysis(t))

	assert.Equal(t, "String literals should not be duplicated", values["rule"].(map[string]any)["name"])

	analysis := values["analysis"].(map[string]any)
	assert.Equal(t, "My-Project_2024", analysis["projectKey"])
	assert.Equal(t, "feature/new-thing", analysis["branch"])
	assert.Nil(t, analysis["pullRequest"], "absent values are present as nil")
	assert.Equal(t, false, analysis["isMainBranch"])

	derived := values["derived"].(map[string]any)
	assert.Equal(t, "my-project-2024", derived["projectSlug"])
	assert.Equal(t, "Define a constant instead of duplicating this literal 3 times.", derived["title"])
	assert.Equal(t, "java:S1192 — src/main/java/App.java:42", derived["description"])
	assert.Equal(t, "HIGH", derived["severity"])
	assert.Equal(t, "src/main/java/App.java", derived["componentPath"])
	assert.Equal(t, "2026-09-15T08:00:00Z", derived["creationDate"])
	assert.Nil(t, derived["closeDate"])
	assert.Equal(t, "https://github.com/org/repo", derived["repositoryUrl"])
	assert.Equal(t, "https://github.com/org/repo/blob/"+testRevision+"/src/main/java/App.java#L42", derived["scmUrl"])
	assert.Equal(t,
		"https://sonar.example.com/project/issues?branch=feature%2Fnew-thing&id=My-Project_2024&issues=AZaBcDeFgHiJkLmNoPqR&open=AZaBcDeFgHiJkLmNoPqR",
		derived["sonarqubeUrl"])

	assert.Equal(t, map[string]any{
		"sonarqube.mia-platform.eu/project": "My-Project_2024",
		"sonarqube.mia-platform.eu/branch":  "feature-new-thing",
		"sonarqube.mia-platform.eu/type":    "CODE_SMELL",
	}, derived["labels"], "only facts that never change: the Catalog keeps metadata from the first write")
	assert.Equal(t, []any{"sonarqube", "design", "bad-practice"}, derived["tags"])

	links := derived["links"].([]any)
	require.Len(t, links, 2)
	assert.Equal(t, sonarqubeLinkTitle, links[0].(map[string]any)["title"])
	assert.Equal(t, scmLinkTitle, links[1].(map[string]any)["title"])
}

func TestIssueValuesEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("a project-level finding gets no scm link", func(t *testing.T) {
		t.Parallel()

		issue := map[string]any{"key": "AZaKey", "rule": "java:S100", "component": "my-project"}
		derived := derivedValues(issue, testAnalysis(t))
		assert.Nil(t, derived["scmUrl"])
		assert.Nil(t, derived["componentPath"])
		assert.Len(t, derived["links"], 1)
		assert.Equal(t, "java:S100", derived["description"])
	})

	t.Run("an issue without a message is titled by its rule", func(t *testing.T) {
		t.Parallel()

		issue := map[string]any{"key": "AZaKey", "rule": "java:S1192"}
		assert.Equal(t, "java:S1192", derivedValues(issue, testAnalysis(t))["title"])
	})

	t.Run("a link survives a project key carrying url syntax", func(t *testing.T) {
		t.Parallel()

		analysis := testAnalysis(t)
		analysis.projectKey = "a&b=c"
		url := sonarqubeIssueURL(map[string]any{"key": "AZaKey"}, analysis)
		assert.Contains(t, url, "id=a%26b%3Dc")
	})

	t.Run("a server served under a context path keeps it", func(t *testing.T) {
		t.Parallel()

		analysis := testAnalysis(t)
		analysis.serverURL = "https://example.com/sonar"
		assert.Contains(t, sonarqubeIssueURL(map[string]any{"key": "AZaKey"}, analysis), "https://example.com/sonar/project/issues?")
	})

	t.Run("a server url that is not http is never linked", func(t *testing.T) {
		t.Parallel()

		analysis := testAnalysis(t)
		analysis.serverURL = "javascript:alert(1)"
		analysis.scm = nil
		derived := derivedValues(map[string]any{"key": "AZaKey"}, analysis)
		assert.Nil(t, derived["sonarqubeUrl"])
		assert.Empty(t, derived["links"])
	})
}
