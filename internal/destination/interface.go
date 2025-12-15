// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package destination

import (
	"context"
	"encoding/json"
)

// Sender defines the contract that any data destination must implement to handle sending
// and deleting operations.
type Sender interface {
	SendData(ctx context.Context, data *Data) error
	DeleteData(ctx context.Context, data *Data) error
}

// Data represents the data to be sent to or deleted from a destination.
type Data struct {
	APIVersion string         `json:"apiVersion"`
	Resource   string         `json:"resource"`
	Name       string         `json:"name"`
	Data       map[string]any `json:"data,omitempty"`
}

// internalData is an alias to avoid infinite recursion in MarshalJSON.
type internalData Data

// MarshalJSON implements json.Marshaler.
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
