// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"context"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// nexusEventHeader is the HTTP header that identifies the Nexus webhook event type.
	nexusEventHeader = "X-Nexus-Webhook-Id"

	// componentEventKey is the X-Nexus-Webhook-Id value for component events.
	componentEventKey = "rm:repository:component"
)

// eventProcessor handles a single Nexus webhook event type.
type eventProcessor interface {
	// process parses the raw webhook body and returns zero or more source.Data entries.
	// Returns an error only for unrecoverable failures (e.g. body parse error).
	// The implementation must NOT send to results directly — that is the dispatcher's job.
	process(ctx context.Context, c *client, host string, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error)
}

// eventProcessors maps X-Nexus-Webhook-Id header values to their processor.
// Register new event types by adding an entry here and creating the
// corresponding processor_*.go file.
var eventProcessors = map[string]eventProcessor{
	componentEventKey: &componentEventProcessor{},
}
