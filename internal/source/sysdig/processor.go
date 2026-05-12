// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sysdig

import (
	"context"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// pipelineFailureAlertKey is the event key that identifies vulnerability
	// pipeline scan notifications from Sysdig.
	pipelineFailureAlertKey = "Pipeline Failure Alerts"
)

// eventProcessor handles a single Sysdig webhook event type.
type eventProcessor interface {
	// process parses the raw webhook body and returns zero or more source.Data
	// entries. Returns an error only for unrecoverable failures (parse error,
	// API failure); the dispatcher logs it and drops the event.
	// The implementation must NOT send to results directly — that is the
	// dispatcher's job.
	process(ctx context.Context, vc *vulnerabilityClient, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error)
}

// eventProcessors maps event type values to their processor.
// Register new event types by adding an entry here and creating the
// corresponding processor_*.go file.
var eventProcessors = map[string]eventProcessor{
	pipelineFailureAlertKey: &vulnerabilityEventProcessor{},
}
