// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

import (
	"context"
	"time"
)

// Extra represents additional configuration for a source data type.
type Extra map[string]any

// MappingExtras maps a mapping name to the extra configuration that mapping declares.
type MappingExtras map[string]Extra

// SyncableSource exposes a pull-based synchronization flow.
type SyncableSource interface {
	// StartSyncProcess kicks off a sync run, pushing data into results or returning an error.
	// typesToSync lists the data types to fetch, each with the extra configuration of
	// every mapping registered for it.
	StartSyncProcess(ctx context.Context, typesToSync map[string]MappingExtras, results chan<- Data) (err error)
}

// EventSource streams data updates as they arrive.
type EventSource interface {
	// StartEventStream begins streaming updates, writing to results or returning an error.
	// typesToStream lists the expected data types, each with the extra configuration of
	// every mapping registered for it.
	StartEventStream(ctx context.Context, typesToStream map[string]MappingExtras, results chan<- Data) (err error)
}

// ClosableSource supports graceful shutdown.
type ClosableSource interface {
	// Close releases resources, respecting the provided timeout.
	Close(ctx context.Context, timeout time.Duration) (err error)
}

type WebhookSource interface {
	// GetWebhook sets up webhooks for the specified data types, sending updates to results or returning an error.
	// typesToStream lists the expected data types, each with the extra configuration of
	// every mapping registered for it.
	GetWebhook(ctx context.Context, typesToStream map[string]MappingExtras, results chan<- Data) (webhook Webhook, err error)
}
