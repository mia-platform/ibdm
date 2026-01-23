// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package service

import (
	"context"
)

type ConsoleServiceInterface interface {
	GetRevision(ctx context.Context, projectID, resourceID string) (map[string]any, error)
}
