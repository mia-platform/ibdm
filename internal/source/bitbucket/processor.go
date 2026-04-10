// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"context"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	bitbucketEventHeader = "X-Event-Key"

	repoPushEventKey             = "repo:push"
	repoUpdatedEventKey          = "repo:updated"
	pullRequestFulfilledEventKey = "pullrequest:fulfilled"
)

// eventProcessor handles a single Bitbucket webhook event type.
type eventProcessor interface {
	// process parses the raw webhook body and returns zero or more source.Data
	// entries. Returns an error only for unrecoverable failures (parse error,
	// API failure); the dispatcher logs it and drops the event.
	// The implementation must NOT send to results directly — that is the
	// dispatcher's job.
	process(ctx context.Context, c *client, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error)
}

// eventProcessors maps X-Event-Key header values to their processor.
// Register new event types by adding an entry here and creating the
// corresponding processor_*.go file.
var eventProcessors = map[string]eventProcessor{
	repoPushEventKey:             &repositoryEventProcessor{},
	repoUpdatedEventKey:          &repositoryEventProcessor{},
	pullRequestFulfilledEventKey: &repositoryEventProcessor{},
}
