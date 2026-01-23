// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package service

import (
	"context"
	"net/http"
)

type consoleServiceInterface interface {
	getClient(ctx context.Context) *http.Client
	DoRequest(ctx context.Context, resource, resourceId string) (map[string]any, error)
}
