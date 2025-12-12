// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package destination

import (
	"context"
)

// Sender defines the contract that any data destination must implement to handle sending
// and deleting operations.
type Sender interface {
	SendData(ctx context.Context, identifier string, spec map[string]any) error
	DeleteData(ctx context.Context, identifier string) error
}
