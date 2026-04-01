// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"context"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	githubEventHeader                          = "X-GitHub-Event"
	repositoryEventHeaderValue                 = "repository"
	pushEventHeaderValue                       = "push"
	personalAccessTokenRequestEventHeaderValue = "personal_access_token_request"
	workflowDispatchEventHeaderValue           = "workflow_dispatch"
	workflowRunEventHeaderValue                = "workflow_run"
)

// eventProcessor handles a single GitHub webhook event type.
type eventProcessor interface {
	// process parses the raw webhook body and returns zero or more source.Data
	// entries. Returns an error only for unrecoverable failures (parse error,
	// API failure); the dispatcher logs it and drops the event.
	// The implementation must NOT send to results directly — that is the
	// dispatcher's job.
	process(ctx context.Context, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error)
}

// eventProcessors is the static registry of event processors.
// Keys are the exact values of the X-GitHub-Event header.
var eventProcessors = map[string]eventProcessor{
	repositoryEventHeaderValue:                 &repositoryEventProcessor{},
	pushEventHeaderValue:                       &pushEventProcessor{},
	personalAccessTokenRequestEventHeaderValue: &personalAccessTokenRequestProcessor{},
	workflowDispatchEventHeaderValue:           &workflowDispatchProcessor{},
	workflowRunEventHeaderValue:                &workflowRunProcessor{},
}
