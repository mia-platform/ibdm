// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import (
	"context"
	"time"

	"github.com/mia-platform/ibdm/internal/mapper"
)

//go:generate ${TOOLS_BIN}/stringer -type=DataOperation -trimprefix Data
type DataOperation int

const (
	// DataOperationUpsert represents an upsert (insert or update) operation for the data source.
	DataOperationUpsert DataOperation = iota
	// DataOperationDelete represents a deletion operation for the data source.
	DataOperationDelete
)

// SourceData encapsulate the values and metadata for a data returned by a data source.
type SourceData struct {
	// Type represents the type of the data returned by the data source. (e.g., "repository", "issue", etc.)
	Type string
	// Operation indicates the operation to be performed on the data (upsert or delete).
	Operation DataOperation
	// Values contains the actual data values as a map of key-value pairs. In case of Delete operation,
	// it must contains at least the keys and values necessary to create the unique identifier of the data.
	Values map[string]any
}

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

// ClosableSource defines the interface for a data source that can be gracefully closed. It will
// be called when the source is being shut down to allow it to release any held resources. It receives
// a context and a timeout duration to ensure the close operation does not hang indefinitely.
// The close method can be called both during normal shutdown or for restarting the source.
type ClosableSource interface {
	// Close will be called to gracefully shut down the data source, releasing any resources it holds.
	Close(ctx context.Context, timeout time.Duration) (err error)
}

type DataDestination interface {
	SendData(ctx context.Context, data mapper.MappedData) error
	DeleteData(ctx context.Context, identifier string) error
}
