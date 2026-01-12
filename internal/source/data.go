// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

import "time"

//go:generate ${TOOLS_BIN}/stringer -type=DataOperation -trimprefix DataOperation
type DataOperation int

const (
	// DataOperationUpsert represents an upsert (insert or update) operation.
	DataOperationUpsert DataOperation = iota
	// DataOperationDelete represents a delete operation.
	DataOperationDelete
)

var (
	nowFunc = time.Now
)

// Data groups the type, operation, and values emitted by a source.
type Data struct {
	// Type describes the kind of entity (e.g., "repository", "issue").
	Type string
	// Operation indicates whether the entity must be upserted or deleted.
	Operation DataOperation
	// Values holds the raw payload. For delete operations, it must contain enough data to reconstruct the identifier.
	Values map[string]any
	// Time indicates the timestamp of the event that generated this data.
	Time time.Time
}

func (d *Data) Timestamp() string {
	dataTime := d.Time
	if dataTime.IsZero() {
		dataTime = nowFunc()
	}

	return dataTime.UTC().Format(time.RFC3339)
}
