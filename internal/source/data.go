// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

//go:generate ${TOOLS_BIN}/stringer -type=DataOperation -trimprefix Data
type DataOperation int

const (
	// DataOperationUpsert represents an upsert (insert or update) operation for the data source.
	DataOperationUpsert DataOperation = iota
	// DataOperationDelete represents a deletion operation for the data source.
	DataOperationDelete
)

// Data encapsulate the values and metadata for a data returned by a data source.
type Data struct {
	// Type represents the type of the data returned by the data source. (e.g., "repository", "issue", etc.)
	Type string
	// Operation indicates the operation to be performed on the data (upsert or delete).
	Operation DataOperation
	// Values contains the actual data values as a map of key-value pairs. In case of Delete operation,
	// it must contains at least the keys and values necessary to create the unique identifier of the data.
	Values map[string]any
}
