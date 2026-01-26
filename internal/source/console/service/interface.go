// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package service

import (
	"context"
)

type ConsoleServiceInterface interface {
	GetProject(ctx context.Context, projectID string) (map[string]any, error)
	GetProjects(ctx context.Context) ([]map[string]any, error)
	GetRevisions(ctx context.Context, projectID string) ([]map[string]any, error)
	GetConfiguration(ctx context.Context, projectID, revisionID string) (map[string]any, error)
}
