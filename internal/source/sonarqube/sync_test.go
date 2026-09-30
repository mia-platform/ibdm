// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

// collect drains results after the sync has returned.
func collect(results chan source.Data) []source.Data {
	close(results)
	data := make([]source.Data, 0, len(results))
	for d := range results {
		data = append(data, d)
	}
	return data
}

func TestStartSyncProcessAllProjects(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		switch r.URL.Path {
		case componentsSearchPath:
			writeJSON(t, w, map[string]any{
				"paging": map[string]any{"total": 2},
				"components": []any{
					map[string]any{"key": "first", "name": "First"},
					map[string]any{"key": "broken", "name": "Broken"},
				},
			})
		case projectBranchesPath:
			writeJSON(t, w, map[string]any{"branches": []any{
				map[string]any{"name": "develop", "isMain": true, "analysisDate": "2026-09-15T10:00:00+0200"},
			}})
		case projectAnalysesPath:
			writeJSON(t, w, map[string]any{"analyses": []any{
				map[string]any{"key": "AZaAnalysis-" + query.Get("project"), "date": "2026-09-15T10:00:00+0200", "revision": testRevision, "projectVersion": "1.2.0"},
			}})
		case qualityGateStatusPath:
			assert.True(t, strings.HasPrefix(query.Get("analysisId"), "AZaAnalysis-"))
			writeJSON(t, w, map[string]any{"projectStatus": map[string]any{"status": "OK", "conditions": []any{
				map[string]any{"metricKey": "new_coverage", "comparator": "LT", "status": "OK", "actualValue": "90", "errorThreshold": "80"},
			}}})
		case qualityGateByProjectPath:
			writeJSON(t, w, map[string]any{"qualityGate": map[string]any{"name": "Sonar way"}})
		case projectLinksPath:
			writeJSON(t, w, map[string]any{"links": []any{map[string]any{"type": "scm", "url": "https://github.com/org/" + query.Get("projectKey")}}})
		case issuesSearchPath:
			assert.False(t, query.Has("branch"), "the main branch is read without naming it")
			if query.Get("components") == "broken" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			issuePage(t, w, []string{"AZa1", "AZa2"}, 2)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	results := make(chan source.Data, 10)
	require.NoError(t, s.StartSyncProcess(t.Context(), map[string]source.Extra{issueType: {}, runType: {}}, results))
	data := collect(results)

	// A project that fails does not cost the others: its run is written, its
	// issues are not.
	require.Len(t, data, 4)
	runs := data[0].Values
	assert.Equal(t, runType, data[0].Type)
	run := runs["run"].(map[string]any)
	assert.Equal(t, "AZaAnalysis-first", run["analysisKey"])
	assert.Equal(t, "1.2.0", run["projectVersion"])
	assert.Equal(t, 2, run["issuesRead"])
	assert.Len(t, run["qualityGateConditions"], 1)
	runAnalysis := runs["analysis"].(map[string]any)
	assert.Equal(t, "OK", runAnalysis["qualityGateStatus"])
	assert.Equal(t, "Sonar way", runAnalysis["qualityGateName"])
	// The key a webhook delivery for the same analysis of the main branch produces.
	assert.Equal(t, "first||2026-09-15T08:00:00Z", runAnalysis["runKey"])

	assert.Equal(t, runType, data[3].Type, "the broken project still has its run")
	data = data[1:3]
	analysis := data[0].Values["analysis"].(map[string]any)
	assert.Equal(t, "first", analysis["projectKey"])
	assert.Equal(t, "First", analysis["projectName"])
	assert.Equal(t, "develop", analysis["branch"])
	assert.Equal(t, true, analysis["isMainBranch"])
	assert.Equal(t, "2026-09-15T08:00:00Z", analysis["analysedAt"])
	assert.Equal(t, s.config.URL, analysis["serverUrl"])

	derived := data[0].Values["derived"].(map[string]any)
	assert.Equal(t, "https://github.com/org/first/blob/"+testRevision+"/src/main/java/App.java#L42", derived["scmUrl"],
		"the revision of the latest analysis is known, so the link points at the commit")
	assert.Equal(t, "first||2026-09-15T08:00:00Z", analysis["runKey"])
}

func TestStartSyncProcessConfiguredProjects(t *testing.T) {
	t.Parallel()

	var searched []string
	var lock sync.Mutex
	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		switch r.URL.Path {
		case componentsSearchPath:
			assert.Fail(t, "the configured projects are not listed")
		case componentsShowPath:
			if query.Get("component") == "unknown" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeJSON(t, w, map[string]any{"component": map[string]any{"key": query.Get("component"), "name": "Named"}})
		case issuesSearchPath:
			lock.Lock()
			searched = append(searched, query.Get("components"))
			lock.Unlock()
			issuePage(t, w, []string{"AZa-" + query.Get("components")}, 1)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	s.config.ProjectKeys = []string{"known", "unknown"}
	s.scm.provider = providerNone

	results := make(chan source.Data, 10)
	require.NoError(t, s.StartSyncProcess(t.Context(), issueTypes, results))
	data := collect(results)

	lock.Lock()
	sort.Strings(searched)
	assert.Equal(t, []string{"known", "unknown"}, searched)
	lock.Unlock()
	require.Len(t, data, 2)
	assert.Equal(t, "Named", data[0].Values["analysis"].(map[string]any)["projectName"])
	assert.Nil(t, data[1].Values["analysis"].(map[string]any)["projectName"], "a project that cannot be read is synced without its name")
}

func TestStartSyncProcessSkipsUnknownTypes(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		assert.Fail(t, "SonarQube must not be read")
	}))

	results := make(chan source.Data, 1)
	require.NoError(t, s.StartSyncProcess(t.Context(), map[string]source.Extra{"other": {}}, results))
	assert.Empty(t, collect(results))
}

func TestStartSyncProcessListingFailure(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	err := s.StartSyncProcess(t.Context(), issueTypes, make(chan source.Data, 1))
	require.ErrorIs(t, err, ErrSonarQubeSource)
}

func TestStartSyncProcessStopsOnCancellation(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case componentsSearchPath:
			writeJSON(t, w, map[string]any{"paging": map[string]any{"total": 1}, "components": []any{map[string]any{"key": "first"}}})
		case issuesSearchPath:
			issuePage(t, w, []string{"AZa1", "AZa2"}, 2)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	ctx, cancel := context.WithCancel(t.Context())
	results := make(chan source.Data) // unbuffered and never read: the send blocks until cancellation
	done := make(chan error, 1)
	go func() { done <- s.StartSyncProcess(ctx, issueTypes, results) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the sync did not stop")
	}
}
