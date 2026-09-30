// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// issuePage answers /api/issues/search with the issues of one page out of total.
func issuePage(t *testing.T, w http.ResponseWriter, keys []string, total int) {
	t.Helper()

	issues := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		issues = append(issues, testIssue(key))
	}
	writeJSON(t, w, map[string]any{
		"paging": map[string]any{"total": total},
		"issues": issues,
		"rules":  testRules(),
	})
}

// keys returns count issue keys starting at offset, with the given prefix.
func keys(prefix string, offset, count int) []string {
	result := make([]string, 0, count)
	for i := range count {
		result = append(result, fmt.Sprintf("%s-%d", prefix, offset+i))
	}
	return result
}

func TestClientIssuesQuery(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		scope       componentScope
		newCodeOnly bool
		expect      map[string]string
		absent      []string
	}{
		"branch": {
			scope:  componentScope{kind: scopeBranch, name: "main"},
			expect: map[string]string{"branch": "main"},
			absent: []string{"pullRequest", "inNewCodePeriod"},
		},
		"pull request": {
			scope:  componentScope{kind: scopePullRequest, name: "42"},
			expect: map[string]string{"pullRequest": "42"},
			absent: []string{"branch"},
		},
		"default scope and new code only": {
			newCodeOnly: true,
			expect:      map[string]string{"inNewCodePeriod": "true"},
			absent:      []string{"branch", "pullRequest"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, issuesSearchPath, r.URL.Path)
				assert.Equal(t, "Bearer "+testToken, r.Header.Get("Authorization"))

				query := r.URL.Query()
				assert.Equal(t, "my-project", query.Get("components"))
				assert.Equal(t, "OPEN,CONFIRMED", query.Get("issueStatuses"))
				assert.Equal(t, "rules", query.Get("additionalFields"))
				assert.Equal(t, "1", query.Get("p"))
				for key, value := range tc.expect {
					assert.Equal(t, value, query.Get(key), key)
				}
				for _, key := range tc.absent {
					assert.False(t, query.Has(key), key)
				}
				issuePage(t, w, []string{"AZa1"}, 1)
			}))
			s.client.newCodeOnly = tc.newCodeOnly

			fetched, err := s.client.issues(t.Context(), "my-project", tc.scope)
			require.NoError(t, err)
			assert.Len(t, fetched.issues, 1)
			assert.Contains(t, fetched.rules, "java:S1192")
			assert.False(t, fetched.truncated)
		})
	}
}

func TestClientIssuesPagination(t *testing.T) {
	t.Parallel()

	var pages []string
	var lock sync.Mutex
	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("p"))
		lock.Lock()
		pages = append(pages, r.URL.Query().Get("p"))
		lock.Unlock()

		// 5 issues, 2 per page; the last page repeats one key, which is de-duplicated.
		switch page {
		case 1:
			issuePage(t, w, []string{"a", "b"}, 5)
		case 2:
			issuePage(t, w, []string{"c", "d"}, 5)
		default:
			issuePage(t, w, []string{"d", "e"}, 5)
		}
	}))
	s.client.pageSize = 2

	fetched, err := s.client.issues(t.Context(), "my-project", componentScope{})
	require.NoError(t, err)
	lock.Lock()
	assert.Equal(t, []string{"1", "2", "3"}, pages)
	lock.Unlock()
	require.Len(t, fetched.issues, 5)
	assert.Equal(t, "e", fetched.issues[4]["key"])
	assert.False(t, fetched.truncated)
}

func TestClientIssuesMaxIssuesTruncates(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("p"))
		issuePage(t, w, keys("k", (page-1)*2, 2), 10)
	}))
	s.client.pageSize = 2
	s.client.maxIssues = 3

	fetched, err := s.client.issues(t.Context(), "my-project", componentScope{})
	require.NoError(t, err)
	assert.Len(t, fetched.issues, 3)
	assert.True(t, fetched.truncated)
}

func TestClientIssuesPastThePaginationCeilingAreReadPerSeverity(t *testing.T) {
	t.Parallel()

	var severities []string
	var lock sync.Mutex
	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		severity := r.URL.Query().Get("impactSeverities")
		if severity == "" {
			// The probe: more than one query can paginate.
			issuePage(t, w, []string{"probe"}, maxPaginatedResults+1)
			return
		}

		lock.Lock()
		severities = append(severities, severity)
		lock.Unlock()
		// Buckets overlap: "shared" is in every one of them.
		issuePage(t, w, []string{"shared", "only-" + severity}, 2)
	}))

	fetched, err := s.client.issues(t.Context(), "my-project", componentScope{})
	require.NoError(t, err)
	lock.Lock()
	assert.Equal(t, impactSeverities, severities, "worst first")
	lock.Unlock()
	// The probe page is not absorbed on this path; every bucket is walked from page 1.
	assert.Len(t, fetched.issues, 1+len(impactSeverities))
	assert.False(t, fetched.truncated)
}

func TestClientIssuesABucketPastTheCeilingIsTruncated(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("p"))
		severity := r.URL.Query().Get("impactSeverities")
		issuePage(t, w, keys(severity, page*sonarqubeMaxPageSize, sonarqubeMaxPageSize), maxPaginatedResults*2)
	}))
	s.client.maxIssues = maxPaginatedResults * 5

	fetched, err := s.client.issues(t.Context(), "my-project", componentScope{})
	require.NoError(t, err)
	assert.True(t, fetched.truncated)
	assert.Len(t, fetched.issues, maxPaginatedResults, "only the BLOCKER bucket was read before stopping")
}

func TestClientIssuesErrors(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"msg":"bad token"}]}`))
	}))

	_, err := s.client.issues(t.Context(), "my-project", componentScope{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "bad token")
}

func TestClientProjectRepositoryURL(t *testing.T) {
	t.Parallel()

	t.Run("the scm link is returned", func(t *testing.T) {
		t.Parallel()

		s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, projectLinksPath, r.URL.Path)
			assert.Equal(t, "my-project", r.URL.Query().Get("projectKey"))
			writeJSON(t, w, map[string]any{"links": []any{
				map[string]any{"type": "homepage", "url": "https://example.com"},
				map[string]any{"type": "SCM", "url": "git@github.com:org/repo.git"},
			}})
		}))

		assert.Equal(t, "git@github.com:org/repo.git", s.client.projectRepositoryURL(t.Context(), "my-project"))
	})

	t.Run("a refused call is no link, not an error", func(t *testing.T) {
		t.Parallel()

		s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))

		assert.Empty(t, s.client.projectRepositoryURL(t.Context(), "my-project"))
	})
}

func TestClientListProjects(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, componentsSearchPath, r.URL.Path)
		assert.Equal(t, projectQualifier, r.URL.Query().Get("qualifiers"))

		page, _ := strconv.Atoi(r.URL.Query().Get("p"))
		components := make([]map[string]any, 0, sonarqubeMaxPageSize)
		count := sonarqubeMaxPageSize
		if page == 2 {
			count = 1
		}
		for i := range count {
			components = append(components, map[string]any{"key": fmt.Sprintf("p-%d-%d", page, i), "name": "Project"})
		}
		writeJSON(t, w, map[string]any{
			"paging":     map[string]any{"total": sonarqubeMaxPageSize + 1},
			"components": components,
		})
	}))

	projects, err := s.client.listProjects(t.Context())
	require.NoError(t, err)
	assert.Len(t, projects, sonarqubeMaxPageSize+1)
}

func TestClientMainBranch(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, projectBranchesPath, r.URL.Path)
		if r.URL.Query().Get("project") == "no-main" {
			writeJSON(t, w, map[string]any{"branches": []any{}})
			return
		}
		writeJSON(t, w, map[string]any{"branches": []any{
			map[string]any{"name": "feature", "isMain": false},
			map[string]any{"name": "develop", "isMain": true, "analysisDate": "2026-09-15T10:00:00+0200"},
		}})
	}))

	main, err := s.client.mainBranch(t.Context(), "my-project")
	require.NoError(t, err)
	assert.Equal(t, "develop", main.Name)

	_, err = s.client.mainBranch(t.Context(), "no-main")
	assert.ErrorIs(t, err, errNoMainBranch)
}
