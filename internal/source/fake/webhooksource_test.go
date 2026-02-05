// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestFakeUnclosableWebhookSource(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	method := http.MethodPost
	path := "/webhook"
	results := make(chan source.Data, 1)
	typesToStream := map[string]source.Extra{"project": {"mode": "full"}}

	expectedData := source.Data{
		Type:      "project",
		Operation: source.DataOperationUpsert,
		Values:    map[string]any{"key": "value"},
	}

	var handlerCalled bool
	handler := func(ctx context.Context, receivedTypes map[string]source.Extra, sourceChan chan<- source.Data) error {
		assert.Equal(t, typesToStream, receivedTypes)
		handlerCalled = true
		sourceChan <- expectedData
		return nil
	}

	webhookSource := NewFakeUnclosableWebhookSource(t, method, path, handler)
	webhook, err := webhookSource.GetWebhook(ctx, typesToStream, results)
	assert.NoError(t, err)
	assert.Equal(t, method, webhook.Method)
	assert.Equal(t, path, webhook.Path)
	assert.NotNil(t, webhook.Handler)

	err = webhook.Handler(ctx, http.Header{}, nil)
	assert.NoError(t, err)
	assert.True(t, handlerCalled)

	select {
	case data := <-results:
		assert.Equal(t, expectedData, data)
	case <-ctx.Done():
		assert.Fail(t, "context cancelled", "error", ctx.Err())
	}
}

func TestFakeUnclosableWebhookSourceHandlerError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	results := make(chan source.Data, 1)
	typesToStream := map[string]source.Extra{"project": {}}
	method := http.MethodPost
	path := "/webhook"
	testErr := assert.AnError

	handler := func(context.Context, map[string]source.Extra, chan<- source.Data) error {
		return testErr
	}

	webhookSource := NewFakeUnclosableWebhookSource(t, method, path, handler)
	webhook, err := webhookSource.GetWebhook(ctx, typesToStream, results)
	assert.NoError(t, err)
	assert.NotNil(t, webhook.Handler)

	err = webhook.Handler(ctx, http.Header{}, nil)
	assert.Equal(t, testErr, err)
}
