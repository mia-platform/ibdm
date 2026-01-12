// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package destination

import (
	"context"
	"encoding/json"
)

// Sender delivers resource mutations to a destination, supporting upsert and delete flows.
type Sender interface {
	SendData(ctx context.Context, data *Data) error
	DeleteData(ctx context.Context, data *Data) error
}

// Data bundles the resource metadata and payload shipped to a destination.
type Data struct {
	APIVersion    string         `json:"apiVersion"`
	Resource      string         `json:"resource"`
	Name          string         `json:"name"`
	Data          map[string]any `json:"data,omitempty"`
	OperationTime string         `json:"operationTime,omitempty"`
}

// internalData breaks the recursion when customizing JSON marshaling.
type internalData Data

// MarshalJSON labels the payload with the operation derived from the data content.
func (d Data) MarshalJSON() ([]byte, error) {
	operation := "upsert"
	if d.Data == nil {
		operation = "delete"
	}

	return json.Marshal(struct {
		internalData

		Operation string `json:"operation"`
	}{
		internalData: internalData(d),
		Operation:    operation,
	})
}
