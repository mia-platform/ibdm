// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"context"

	"github.com/mia-platform/ibdm/internal/source"
)

// eventProcessor defines the contract for processing a specific GitLab webhook
// event type. Each implementation lives in its own processor_*.go file.
type eventProcessor interface {
	process(ctx context.Context, c *gitLabClient, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error)
}

// eventProcessors maps X-Gitlab-Event header values to their processor.
// Register new event types by adding an entry here and creating the
// corresponding processor_*.go file.
var eventProcessors = map[string]eventProcessor{
	pipelineHookHeaderValue: &pipelineEventProcessor{},
	pushHookHeaderValue:     &pushEventProcessor{},
}
