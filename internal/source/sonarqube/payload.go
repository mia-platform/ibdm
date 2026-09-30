// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	// statusSuccess is the task status of an analysis whose issues are worth
	// reading. A FAILED task never completed: reading the project would record
	// the previous analysis' findings as if they were this one's.
	statusSuccess = "SUCCESS"
	// branchTypePullRequest is the branch.type of a pull request analysis,
	// whose findings live under pullRequest=, not branch=.
	branchTypePullRequest = "PULL_REQUEST"
)

// webhookPayload is the body SonarQube posts when an analysis finishes.
//
// It announces the analysis and reports the quality gate; it does not carry
// the findings, which are read back from /api/issues/search.
type webhookPayload struct {
	ServerURL   string              `json:"serverUrl"`
	TaskID      string              `json:"taskId"`
	Status      string              `json:"status"`
	AnalysedAt  string              `json:"analysedAt"`
	Revision    string              `json:"revision"`
	Project     payloadProject      `json:"project"`
	Branch      *payloadBranch      `json:"branch"`
	QualityGate *payloadQualityGate `json:"qualityGate"`
	// Properties holds the sonar.analysis.* values the scan was run with. A CI
	// job can pass the repository URL through them.
	Properties map[string]any `json:"properties"`
}

// payloadProject is the analysed project.
type payloadProject struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// payloadBranch is the analysed ref.
type payloadBranch struct {
	// Name is the branch name, or the pull request key when Type says so.
	Name   string `json:"name"`
	Type   string `json:"type"`
	IsMain *bool  `json:"isMain"`
}

// payloadQualityGate is the quality gate as it stood after the analysis.
type payloadQualityGate struct {
	Name       string           `json:"name"`
	Status     string           `json:"status"`
	Conditions []map[string]any `json:"conditions"`
}

// parseWebhookPayload deserializes a webhook body. Unknown fields are
// tolerated; a missing project key is not, because nothing can be read without it.
func parseWebhookPayload(body []byte) (*webhookPayload, error) {
	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse webhook body: %w", err)
	}
	if strings.TrimSpace(payload.Project.Key) == "" {
		return nil, errors.New("webhook body carries no project.key")
	}
	return &payload, nil
}

// isSuccessfulAnalysis reports whether the delivery is about a finished analysis.
func (p *webhookPayload) isSuccessfulAnalysis() bool {
	return strings.EqualFold(p.Status, statusSuccess)
}

// property returns a sonar.analysis.* property when it is a string.
func (p *webhookPayload) property(key string) string {
	value, _ := p.Properties[key].(string)
	return value
}

// scope returns the ref whose issues the delivery is about.
func (p *webhookPayload) scope() componentScope {
	if p.Branch == nil {
		return componentScope{}
	}
	name := strings.TrimSpace(p.Branch.Name)
	if name == "" {
		return componentScope{}
	}
	if strings.EqualFold(p.Branch.Type, branchTypePullRequest) {
		return componentScope{kind: scopePullRequest, name: name}
	}
	return componentScope{kind: scopeBranch, name: name}
}

// scopeKind tells which ref an analysis covers.
type scopeKind int

const (
	// scopeDefault names no ref: SonarQube answers for the main branch.
	scopeDefault scopeKind = iota
	scopeBranch
	scopePullRequest
)

// componentScope is the ref to read issues for, in the spelling
// /api/issues/search expects. Asking for the wrong kind silently returns the
// other ref's issues.
type componentScope struct {
	kind scopeKind
	name string
}

// apply adds the ref parameter to a search query.
func (s componentScope) apply(query url.Values) {
	switch s.kind {
	case scopeBranch:
		query.Set("branch", s.name)
	case scopePullRequest:
		query.Set("pullRequest", s.name)
	case scopeDefault:
	}
}

// branch returns the branch name for a branch analysis.
func (s componentScope) branch() string {
	if s.kind == scopeBranch {
		return s.name
	}
	return ""
}

// pullRequest returns the pull request key for a pull request analysis.
func (s componentScope) pullRequest() string {
	if s.kind == scopePullRequest {
		return s.name
	}
	return ""
}
