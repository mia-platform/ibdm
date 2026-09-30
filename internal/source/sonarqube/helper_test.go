// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testToken    = "sonarqube-token-for-tests"
	testSecret   = "webhook-secret-for-tests"
	testRevision = "0123456789abcdef0123456789abcdef01234567"
)

// newTestSource returns a Source reading from a test server backed by handler.
func newTestSource(t *testing.T, handler http.Handler) *Source {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config{
		URL:           server.URL,
		Token:         testToken,
		HTTPTimeout:   5 * time.Second,
		IssueStatuses: "OPEN,CONFIRMED",
		MaxIssues:     1000,
		SCMProvider:   "auto",
	}

	return &Source{
		config: cfg,
		webhookConfig: webhookConfig{
			WebhookPath:   "/sonarqube/webhook",
			WebhookSecret: testSecret,
		},
		scm: scmSettings{
			provider: providerAuto,
		},
		client: newClient(cfg),
	}
}

// writeJSON encodes body as the JSON answer of a test handler.
func writeJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(body))
}

// testIssue returns an issue as /api/issues/search returns it.
func testIssue(key string) map[string]any {
	return map[string]any{
		"key":                        key,
		"rule":                       "java:S1192",
		"severity":                   "MAJOR",
		"component":                  "my-project:src/main/java/App.java",
		"line":                       float64(42),
		"hash":                       "a1b2c3",
		"textRange":                  map[string]any{"startLine": float64(42), "endLine": float64(42), "startOffset": float64(4), "endOffset": float64(19)},
		"status":                     "OPEN",
		"issueStatus":                "OPEN",
		"message":                    "Define a constant instead of duplicating this literal 3 times.",
		"effort":                     "8min",
		"author":                     "dev@example.com",
		"tags":                       []any{"design", "Bad Practice"},
		"creationDate":               "2026-09-15T10:00:00+0200",
		"updateDate":                 "2026-09-16T10:00:00+0200",
		"type":                       "CODE_SMELL",
		"scope":                      "MAIN",
		"quickFixAvailable":          false,
		"cleanCodeAttribute":         "DISTINCT",
		"cleanCodeAttributeCategory": "ADAPTABLE",
		"impacts": []any{
			map[string]any{"softwareQuality": "MAINTAINABILITY", "severity": "LOW"},
			map[string]any{"softwareQuality": "SECURITY", "severity": "HIGH"},
		},
	}
}

// testRules returns the rules block matching testIssue.
func testRules() []map[string]any {
	return []map[string]any{
		{"key": "java:S1192", "name": "String literals should not be duplicated", "langName": "Java"},
	}
}
