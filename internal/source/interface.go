// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

import (
	"context"
)

// SyncableSource defines the interface for a data source that supports synchronization operations.
type SyncableSource interface {
	// StartSyncProcess will be called to initiate a synchronization process for the data source.
	// It receives a channel through which it can send the synched data or it can return an error
	// if the synchronization process fails. typesToSync is a list of data types that need to be
	// synchronized.
	StartSyncProcess(ctx context.Context, typesToSync []string, results chan<- SourceData) (err error)
}

// EventSource defines the interface for a data source that uses event-driven mechanisms to handle
// data updates.
type EventSource interface {
	// StartEventStream will be called to initiate an event stream for the data source.
	// It receives a channel through which it can send the incoming data or it can return an error
	// if the event stream fails. typesToStream is the list of data types that is expected to be
	// returned.
	StartEventStream(ctx context.Context, typesToStream []string, results chan<- SourceData) (err error)
}
