// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustTarget(t *testing.T, repositoryURL string, p provider) *scmTarget {
	t.Helper()

	target, reason := newSCMTarget(repositoryURL, testRevision, "main", scmSettings{provider: p})
	require.NotNil(t, target, reason)
	return target
}

func TestSCMTargetEachForgeGetsItsOwnSpelling(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		repositoryURL string
		provider      provider
		expected      string
	}{
		"gitlab":           {"https://gitlab.example.com/group/repo", providerGitLab, "https://gitlab.example.com/group/repo/-/blob/{r}/src/App.java#L42"},
		"github":           {"https://github.com/org/repo", providerGitHub, "https://github.com/org/repo/blob/{r}/src/App.java#L42"},
		"bitbucket":        {"https://bitbucket.org/team/repo", providerBitbucket, "https://bitbucket.org/team/repo/src/{r}/src/App.java#lines-42"},
		"bitbucket server": {"https://bitbucket.example.com/projects/TEAM/repos/repo", providerBitbucketServer, "https://bitbucket.example.com/projects/TEAM/repos/repo/browse/src/App.java?at={r}#42"},
		"azure devops":     {"https://dev.azure.com/org/project/_git/repo", providerAzureDevOps, "https://dev.azure.com/org/project/_git/repo?path=/src/App.java&version=GC{r}&line=42"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			url, ok := mustTarget(t, tc.repositoryURL, tc.provider).url("src/App.java", 42)
			require.True(t, ok)
			assert.Equal(t, strings.ReplaceAll(tc.expected, "{r}", testRevision), url)
		})
	}
}

func TestSCMTargetURL(t *testing.T) {
	t.Parallel()

	t.Run("an issue without a line links to the file", func(t *testing.T) {
		t.Parallel()

		url, ok := mustTarget(t, "https://github.com/org/repo", providerGitHub).url("src/App.java", 0)
		require.True(t, ok)
		assert.Equal(t, "https://github.com/org/repo/blob/"+testRevision+"/src/App.java", url)
	})

	t.Run("a path with spaces is escaped per segment", func(t *testing.T) {
		t.Parallel()

		url, ok := mustTarget(t, "https://github.com/org/repo", providerGitHub).url("src/main/My Class.java", 7)
		require.True(t, ok)
		assert.True(t, strings.HasSuffix(url, "/src/main/My%20Class.java#L7"), url)
	})

	t.Run("an empty path has no url", func(t *testing.T) {
		t.Parallel()

		_, ok := mustTarget(t, "https://github.com/org/repo", providerGitHub).url("/", 1)
		assert.False(t, ok)
	})
}

func TestNewSCMTarget(t *testing.T) {
	t.Parallel()

	t.Run("the commit is preferred over the branch", func(t *testing.T) {
		t.Parallel()

		onBranch, _ := newSCMTarget("https://github.com/org/repo", "", "release/2.0", scmSettings{provider: providerGitHub})
		require.NotNil(t, onBranch)
		url, _ := onBranch.url("a.java", 0)
		// The slash in the branch name is escaped: it is one path component.
		assert.Equal(t, "https://github.com/org/repo/blob/release%2F2.0/a.java", url)

		onCommit := mustTarget(t, "https://github.com/org/repo", providerGitHub)
		url, _ = onCommit.url("a.java", 0)
		assert.Contains(t, url, testRevision)
	})

	t.Run("the host decides when the provider is auto", func(t *testing.T) {
		t.Parallel()

		url, _ := mustTarget(t, "https://github.com/org/repo", providerAuto).url("a.java", 1)
		assert.Contains(t, url, "/blob/")
	})

	t.Run("an unrecognisable host writes no link rather than a guess", func(t *testing.T) {
		t.Parallel()

		target, reason := newSCMTarget("https://git.example.com/group/repo", testRevision, "", scmSettings{provider: providerAuto})
		assert.Nil(t, target)
		assert.Contains(t, reason, "SONARQUBE_SCM_PROVIDER")
	})

	t.Run("a custom template wins over the built-in shapes", func(t *testing.T) {
		t.Parallel()

		target, _ := newSCMTarget("https://git.example.com/group/repo", testRevision, "", scmSettings{
			provider:    providerCustom,
			urlTemplate: "{repo}/files/{ref}/{path}?highlight={line}",
		})
		require.NotNil(t, target)
		url, _ := target.url("src/App.java", 9)
		assert.Equal(t, "https://git.example.com/group/repo/files/"+testRevision+"/src/App.java?highlight=9", url)
		url, _ = target.url("src/App.java", 0)
		assert.Equal(t, "https://git.example.com/group/repo/files/"+testRevision+"/src/App.java", url)
	})

	t.Run("custom without a template writes no link", func(t *testing.T) {
		t.Parallel()

		target, reason := newSCMTarget("https://git.example.com/group/repo", testRevision, "", scmSettings{provider: providerCustom})
		assert.Nil(t, target)
		assert.Contains(t, reason, "SONARQUBE_SCM_URL_TEMPLATE")
	})

	t.Run("no reference, no repository or provider none mean no link", func(t *testing.T) {
		t.Parallel()

		target, _ := newSCMTarget("https://github.com/org/repo", "", "", scmSettings{provider: providerGitHub})
		assert.Nil(t, target)
		target, _ = newSCMTarget("", testRevision, "", scmSettings{provider: providerGitHub})
		assert.Nil(t, target)
		target, _ = newSCMTarget("https://github.com/org/repo", testRevision, "", scmSettings{provider: providerNone})
		assert.Nil(t, target)
	})

	t.Run("an azure link is refused without a revision", func(t *testing.T) {
		t.Parallel()

		target, reason := newSCMTarget("https://dev.azure.com/org/project/_git/repo", "", "main", scmSettings{provider: providerAzureDevOps})
		assert.Nil(t, target)
		assert.Contains(t, reason, "Azure DevOps")

		target, _ = newSCMTarget("https://dev.azure.com/org/project/_git/repo", "", "main", scmSettings{provider: providerAuto})
		assert.Nil(t, target)
	})
}

func TestNormaliseRepositoryURL(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		raw      string
		expected string
		ok       bool
	}{
		"git suffix":      {"https://github.com/org/repo.git", "https://github.com/org/repo", true},
		"trailing slash":  {"https://github.com/org/repo/", "https://github.com/org/repo", true},
		"maven scm":       {"scm:git:https://github.com/org/repo.git", "https://github.com/org/repo", true},
		"ssh shorthand":   {"git@github.com:org/repo.git", "https://github.com/org/repo", true},
		"ssh url":         {"ssh://git@github.com/org/repo", "", false},
		"blank":           {"   ", "", false},
		"not a url":       {"repo", "", false},
		"javascript link": {"javascript:alert(1)", "", false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			normalised, ok := normaliseRepositoryURL(tc.raw)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.expected, normalised)
		})
	}
}

func TestParseProvider(t *testing.T) {
	t.Parallel()

	p, err := parseProvider("")
	require.NoError(t, err)
	assert.Equal(t, providerAuto, p)

	p, err = parseProvider("Bitbucket_Server")
	require.NoError(t, err)
	assert.Equal(t, providerBitbucketServer, p)

	_, err = parseProvider("unknown-forge")
	assert.ErrorIs(t, err, ErrInvalidEnvVariable)
}

func TestSplitURLTemplate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		template string
		expected scmTemplates
	}{
		"no line":                {template: "{repo}/src/{ref}/{path}", expected: scmTemplates{file: "{repo}/src/{ref}/{path}"}},
		"fragment":               {template: "{repo}/blob/{ref}/{path}#L{line}", expected: githubTemplates},
		"query":                  {template: "{repo}/files/{ref}/{path}?highlight={line}", expected: scmTemplates{file: "{repo}/files/{ref}/{path}", line: "?highlight={line}"}},
		"fragment after a query": {template: "{repo}/browse/{path}?at={ref}#{line}", expected: bitbucketServerTemplates},
		"query parameter":        {template: "{repo}?path=/{path}&version=GC{ref}&line={line}", expected: azureDevOpsTemplates},
		"no separator":           {template: "{repo}/{path}/{line}", expected: scmTemplates{file: "{repo}/{path}/", line: "{line}"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.expected, splitURLTemplate(tc.template))
		})
	}
}
