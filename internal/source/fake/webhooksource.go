// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"net/http"
	"testing"

	"github.com/mia-platform/ibdm/internal/source"
)

var _ source.WebhookSource = &unclosableWebhookSource{}

// unclosableWebhookSource simulates a WebhookSource without close support.
type unclosableWebhookSource struct {
	tb testing.TB

	method  string
	path    string
	handler func(context.Context, map[string]source.Extra, chan<- source.Data) error
}

// NewFakeUnclosableWebhookSource returns a WebhookSource without close capabilities.
func NewFakeUnclosableWebhookSource(tb testing.TB, method, path string, handler func(context.Context, map[string]source.Extra, chan<- source.Data) error) source.WebhookSource {
	tb.Helper()

	return &unclosableWebhookSource{
		tb:      tb,
		method:  method,
		path:    path,
		handler: handler,
	}
}

// GetWebhook pushes queued events and blocks until Close is invoked or the context ends.
func (f *unclosableWebhookSource) GetWebhook(ctx context.Context, typesToFilter map[string]source.Extra, sourceChan chan<- source.Data) (webhook source.Webhook, err error) {
	f.tb.Helper()
	return source.Webhook{
		Method: f.method,
		Path:   f.path,
		Handler: func(ctx context.Context, _ http.Header, _ []byte) error {
			return f.handler(ctx, typesToFilter, sourceChan)
		},
	}, nil
}

var _ source.WebhookSource = &errorWebhookSource{}

type errorWebhookSource struct {
	tb  testing.TB
	err error
}

// NewFakeWebhookSourceWithError returns a WebhookSource that always fails.
func NewFakeWebhookSourceWithError(tb testing.TB, err error) source.WebhookSource {
	tb.Helper()

	return &errorWebhookSource{
		tb:  tb,
		err: err,
	}
}

// GetWebhook returns the configured error.
func (e *errorWebhookSource) GetWebhook(_ context.Context, _ map[string]source.Extra, _ chan<- source.Data) (source.Webhook, error) {
	e.tb.Helper()
	return source.Webhook{}, e.err
}
