// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

const testPayload = `{
  "serverUrl": "https://sonar.example.com",
  "taskId": "AZaTaskId",
  "status": "SUCCESS",
  "analysedAt": "2026-09-15T10:00:00+0200",
  "revision": "0123456789abcdef0123456789abcdef01234567",
  "project": { "key": "my-project", "name": "My Project" },
  "branch": { "name": "main", "type": "BRANCH", "isMain": true },
  "qualityGate": { "name": "Sonar way", "status": "ERROR", "conditions": [] },
  "properties": { "sonar.analysis.repoUrl": "https://github.com/org/repo" },
  "aFieldAFutureVersionAdded": true
}`

// sign returns the signature SonarQube sends for body.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// signedHeaders returns headers carrying a valid signature for body.
func signedHeaders(body []byte) http.Header {
	headers := http.Header{}
	headers.Set(sonarSignatureHeader, sign(body, testSecret))
	return headers
}

var issueTypes = map[string]source.Extra{issueType: {}}

func TestGetWebhookRequiresASecret(t *testing.T) {
	t.Parallel()

	s := &Source{webhookConfig: webhookConfig{WebhookPath: "/sonarqube/webhook"}}
	_, err := s.GetWebhook(t.Context(), issueTypes, nil)
	require.ErrorIs(t, err, ErrSonarQubeSource)
	require.ErrorIs(t, err, ErrMissingEnvVariable)
	assert.Contains(t, err.Error(), "SONARQUBE_WEBHOOK_SECRET")

	s.webhookConfig.AllowUnsigned = true
	webhook, err := s.GetWebhook(t.Context(), issueTypes, nil)
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, webhook.Method)
	assert.Equal(t, "/sonarqube/webhook", webhook.Path)
}

func TestWebhookHandlerSignature(t *testing.T) {
	t.Parallel()

	body := []byte(`{"status":"FAILED","project":{"key":"my-project"}}`)

	tests := map[string]struct {
		headers   http.Header
		secret    string
		unsigned  bool
		expectErr bool
	}{
		"valid signature": {
			headers: signedHeaders(body),
			secret:  testSecret,
		},
		"missing signature": {
			headers:   http.Header{},
			secret:    testSecret,
			expectErr: true,
		},
		"signature over another body": {
			headers:   http.Header{sonarSignatureHeader: {sign([]byte(`{}`), testSecret)}},
			secret:    testSecret,
			expectErr: true,
		},
		"signature with another secret": {
			headers:   http.Header{sonarSignatureHeader: {sign(body, "another-secret")}},
			secret:    testSecret,
			expectErr: true,
		},
		"github spelling is rejected": {
			headers:   http.Header{sonarSignatureHeader: {"sha256=" + sign(body, testSecret)}},
			secret:    testSecret,
			expectErr: true,
		},
		"not hex": {
			headers:   http.Header{sonarSignatureHeader: {"not-hex"}},
			secret:    testSecret,
			expectErr: true,
		},
		"a secret is verified even when unsigned deliveries are allowed": {
			headers:   http.Header{},
			secret:    testSecret,
			unsigned:  true,
			expectErr: true,
		},
		"unsigned delivery accepted without a secret when allowed": {
			headers:  http.Header{},
			unsigned: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := &Source{webhookConfig: webhookConfig{WebhookSecret: tc.secret, AllowUnsigned: tc.unsigned}}
			webhook, err := s.GetWebhook(t.Context(), issueTypes, nil)
			require.NoError(t, err)

			err = webhook.Handler(t.Context(), tc.headers, body)
			if tc.expectErr {
				require.ErrorIs(t, err, ErrSonarQubeSource)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestWebhookHandlerRejectsABodyThatIsNotAPayload(t *testing.T) {
	t.Parallel()

	s := &Source{webhookConfig: webhookConfig{WebhookSecret: testSecret}}
	webhook, err := s.GetWebhook(t.Context(), issueTypes, nil)
	require.NoError(t, err)

	for _, body := range [][]byte{[]byte(`not json`), []byte(`{"status":"SUCCESS"}`)} {
		err := webhook.Handler(t.Context(), signedHeaders(body), body)
		assert.ErrorIs(t, err, ErrSonarQubeSource, string(body))
	}
}

func TestWebhookHandlerIgnoresWhatItHasNothingToReadFor(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body  []byte
		types map[string]source.Extra
	}{
		"a failed analysis": {
			body:  []byte(`{"status":"FAILED","project":{"key":"my-project"}}`),
			types: issueTypes,
		},
		"an unmapped issue type": {
			body:  []byte(testPayload),
			types: map[string]source.Extra{"other": {}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := newTestSource(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				assert.Fail(t, "SonarQube must not be read")
			}))
			results := make(chan source.Data, 1)
			webhook, err := s.GetWebhook(t.Context(), tc.types, results)
			require.NoError(t, err)

			require.NoError(t, webhook.Handler(t.Context(), signedHeaders(tc.body), tc.body))
			assert.Never(t, func() bool { return len(results) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
		})
	}
}

func TestWebhookHandlerEmitsTheIssuesOfTheAnalysis(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case issuesSearchPath:
			assert.Equal(t, "main", r.URL.Query().Get("branch"))
			issuePage(t, w, []string{"AZa1", "AZa2"}, 2)
		default:
			assert.Fail(t, "unexpected call: the repository comes from the analysis property", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	s.config.PublicURL = "https://public.sonar.example.com"

	results := make(chan source.Data, 2)
	webhook, err := s.GetWebhook(t.Context(), issueTypes, results)
	require.NoError(t, err)

	body := []byte(testPayload)
	require.NoError(t, webhook.Handler(t.Context(), signedHeaders(body), body))

	data := make([]source.Data, 0, 2)
	for range 2 {
		select {
		case d := <-results:
			data = append(data, d)
		case <-time.After(5 * time.Second):
			require.FailNow(t, "the issues were not emitted")
		}
	}

	first := data[0]
	assert.Equal(t, issueType, first.Type)
	assert.Equal(t, source.DataOperationUpsert, first.Operation)
	assert.Equal(t, time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC), first.Time.UTC(), "the event time is the analysis time")

	analysis := first.Values["analysis"].(map[string]any)
	assert.Equal(t, "https://public.sonar.example.com", analysis["serverUrl"], "the public URL wins over the payload's")
	assert.Equal(t, "my-project", analysis["projectKey"])
	assert.Equal(t, "My Project", analysis["projectName"])
	assert.Equal(t, "main", analysis["branch"])
	assert.Equal(t, true, analysis["isMainBranch"])
	assert.Equal(t, "AZaTaskId", analysis["taskId"])
	assert.Equal(t, "2026-09-15T08:00:00Z", analysis["analysedAt"])
	assert.Equal(t, "ERROR", analysis["qualityGateStatus"])
	assert.Equal(t, "Sonar way", analysis["qualityGateName"])

	derived := first.Values["derived"].(map[string]any)
	assert.Equal(t, "https://github.com/org/repo/blob/"+testRevision+"/src/main/java/App.java#L42", derived["scmUrl"],
		"the link points at the analysed commit, not the branch")
}

func TestProcessAnalysisOfAPullRequest(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case issuesSearchPath:
			assert.Equal(t, "42", r.URL.Query().Get("pullRequest"))
			assert.False(t, r.URL.Query().Has("branch"))
			issuePage(t, w, []string{"AZa1"}, 1)
		case projectLinksPath:
			writeJSON(t, w, map[string]any{"links": []any{map[string]any{"type": "scm", "url": "scm:git:https://github.com/org/repo.git"}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	payload, err := parseWebhookPayload([]byte(`{
		"serverUrl": "https://sonar.example.com",
		"status": "SUCCESS",
		"project": { "key": "my-project" },
		"branch": { "name": "42", "type": "PULL_REQUEST", "isMain": false }
	}`))
	require.NoError(t, err)

	results := make(chan source.Data, 1)
	require.NoError(t, s.processAnalysis(t.Context(), payload, issueTypes, results))
	data := <-results

	analysis := data.Values["analysis"].(map[string]any)
	assert.Nil(t, analysis["branch"])
	assert.Equal(t, "42", analysis["pullRequest"])
	assert.Equal(t, "https://sonar.example.com", analysis["serverUrl"])

	derived := data.Values["derived"].(map[string]any)
	// The repository is known from the project links, but a pull request key
	// names nothing in it and there is no revision: no line link.
	assert.Nil(t, derived["scmUrl"])
	assert.Nil(t, derived["repositoryUrl"])
}

func TestPayloadScope(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body   string
		expect componentScope
	}{
		"a branch analysis reads that branch": {
			body:   `{"status":"SUCCESS","project":{"key":"p"},"branch":{"name":"main","type":"BRANCH"}}`,
			expect: componentScope{kind: scopeBranch, name: "main"},
		},
		"a pull request analysis reads the pull request": {
			body:   `{"status":"SUCCESS","project":{"key":"p"},"branch":{"name":"42","type":"PULL_REQUEST"}}`,
			expect: componentScope{kind: scopePullRequest, name: "42"},
		},
		"no branch falls back to the main branch": {
			body:   `{"status":"SUCCESS","project":{"key":"p"}}`,
			expect: componentScope{},
		},
		"a blank branch name falls back to the main branch": {
			body:   `{"status":"SUCCESS","project":{"key":"p"},"branch":{"name":"  "}}`,
			expect: componentScope{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			payload, err := parseWebhookPayload([]byte(tc.body))
			require.NoError(t, err)
			assert.True(t, payload.isSuccessfulAnalysis())
			assert.Equal(t, tc.expect, payload.scope())
		})
	}
}

var issueAndRunTypes = map[string]source.Extra{issueType: {}, runType: {}}

// receive reads count data from results, failing when they do not arrive.
func receive(t *testing.T, results chan source.Data, count int) []source.Data {
	t.Helper()

	data := make([]source.Data, 0, count)
	for range count {
		select {
		case d := <-results:
			data = append(data, d)
		case <-time.After(5 * time.Second):
			require.FailNow(t, "the data were not emitted", "received %d of %d", len(data), count)
		}
	}
	return data
}

func TestWebhookHandlerEmitsTheRunBeforeItsIssues(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != issuesSearchPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		issuePage(t, w, []string{"AZa1", "AZa2"}, 2)
	}))
	results := make(chan source.Data, 3)
	webhook, err := s.GetWebhook(t.Context(), issueAndRunTypes, results)
	require.NoError(t, err)

	body := []byte(`{
		"serverUrl": "https://sonar.example.com", "taskId": "AZaTask", "status": "SUCCESS",
		"analysedAt": "2026-09-15T10:00:00+0200", "revision": "` + testRevision + `",
		"project": { "key": "my-project" },
		"branch": { "name": "main", "type": "BRANCH", "isMain": true },
		"qualityGate": { "name": "Sonar way", "status": "ERROR", "conditions": [
			{ "metric": "new_coverage", "operator": "LESS_THAN", "value": "82.5", "status": "ERROR", "errorThreshold": "85" }
		] }
	}`)
	require.NoError(t, webhook.Handler(t.Context(), signedHeaders(body), body))
	data := receive(t, results, 3)

	require.Equal(t, runType, data[0].Type, "the run comes first: the issues point at it")
	run := data[0].Values["run"].(map[string]any)
	assert.Equal(t, "SUCCESS", run["status"])
	assert.Equal(t, 2, run["issuesRead"])
	assert.Len(t, run["qualityGateConditions"], 1)

	runKey := data[0].Values["analysis"].(map[string]any)["runKey"]
	assert.Equal(t, "my-project||2026-09-15T08:00:00Z", runKey)
	for _, issue := range data[1:] {
		assert.Equal(t, issueType, issue.Type)
		assert.Equal(t, runKey, issue.Values["analysis"].(map[string]any)["runKey"], "each issue carries the key of its run")
	}
}

func TestWebhookHandlerRecordsAFailedAnalysisAsARunOnly(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The repository is still resolved: the run records it.
		assert.NotEqual(t, issuesSearchPath, r.URL.Path, "the issues of a failed analysis are not read")
		w.WriteHeader(http.StatusNotFound)
	}))
	results := make(chan source.Data, 2)
	webhook, err := s.GetWebhook(t.Context(), issueAndRunTypes, results)
	require.NoError(t, err)

	body := []byte(`{"serverUrl":"https://sonar.example.com","taskId":"AZaTask","status":"FAILED","project":{"key":"my-project"}}`)
	require.NoError(t, webhook.Handler(t.Context(), signedHeaders(body), body))

	data := receive(t, results, 1)
	assert.Equal(t, runType, data[0].Type)
	assert.Equal(t, "FAILED", data[0].Values["run"].(map[string]any)["status"])
	assert.Nil(t, data[0].Values["run"].(map[string]any)["issuesRead"])
	assert.Never(t, func() bool { return len(results) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
}

func TestWebhookHandlerWithoutRunsMappedRelatesNoIssue(t *testing.T) {
	t.Parallel()

	s := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != issuesSearchPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		issuePage(t, w, []string{"AZa1"}, 1)
	}))
	results := make(chan source.Data, 2)
	webhook, err := s.GetWebhook(t.Context(), issueTypes, results)
	require.NoError(t, err)

	body := []byte(testPayload)
	require.NoError(t, webhook.Handler(t.Context(), signedHeaders(body), body))

	data := receive(t, results, 1)
	assert.Equal(t, issueType, data[0].Type)
	assert.Nil(t, data[0].Values["analysis"].(map[string]any)["runKey"], "no run is written, so none is pointed at")
}
