// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

import (
	"context"
	"net/http"
)

type WebhookHandler func(ctx context.Context, headers http.Header, body []byte) error

type Webhook struct {
	Method  string
	Path    string
	Handler WebhookHandler
}
