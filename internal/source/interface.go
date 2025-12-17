// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

import (
	"context"
	"time"
)

// SyncableSource exposes a pull-based synchronization flow.
type SyncableSource interface {
	// StartSyncProcess kicks off a sync run, pushing data into results or returning an error.
	// typesToSync lists the data types to fetch.
	StartSyncProcess(ctx context.Context, typesToSync []string, results chan<- Data) (err error)
}

// EventSource streams data updates as they arrive.
type EventSource interface {
	// StartEventStream begins streaming updates, writing to results or returning an error.
	// typesToStream lists the expected data types.
	StartEventStream(ctx context.Context, typesToStream []string, results chan<- Data) (err error)
}

// ClosableSource supports graceful shutdown.
type ClosableSource interface {
	// Close releases resources, respecting the provided timeout.
	Close(ctx context.Context, timeout time.Duration) (err error)
}
