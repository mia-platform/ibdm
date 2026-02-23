// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"time"
)

const (
	// pipelineEventKind is the object_kind value for pipeline webhook events.
	pipelineEventKind = "pipeline"

	// gitlabEventHeader is the header name carrying the event type.
	gitlabEventHeader = "X-Gitlab-Event"

	// gitlabTokenHeader is the header name carrying the plain-text secret token.
	//nolint:gosec // this is a header name, not a credential value
	gitlabTokenHeader = "X-Gitlab-Token"

	// pipelineHookHeaderValue is the expected value of X-Gitlab-Event for pipeline events.
	pipelineHookHeaderValue = "Pipeline Hook"
)

// pipelineEvent represents a GitLab pipeline webhook payload.
type pipelineEvent struct {
	ObjectKind       string         `json:"object_kind"`       //nolint:tagliatelle // GitLab API uses snake_case
	ObjectAttributes map[string]any `json:"object_attributes"` //nolint:tagliatelle // GitLab API uses snake_case

	// rawValues holds the full decoded payload used as source.Data.Values.
	rawValues map[string]any
	project   map[string]any
}

// EventTime returns the time of the pipeline event. It reads the updated_at field
// from object_attributes when present, and falls back to time.Now().
func (e *pipelineEvent) EventTime() time.Time {
	if e.ObjectAttributes == nil {
		return time.Now()
	}

	if updatedAt, ok := e.ObjectAttributes["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			return t
		}
	}

	return time.Now()
}

// ToValues returns the full webhook payload as a generic map, suitable for use
// as source.Data.Values.
func (e *pipelineEvent) ToValues() map[string]any {
	return e.rawValues
}
