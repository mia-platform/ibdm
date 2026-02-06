// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddRouteRegistersHandler(t *testing.T) {
	t.Parallel()

	server := NewFakeServer(t, http.MethodPost, "/webhook")

	handled := false
	handler := func(ctx context.Context, headers http.Header, body []byte) error {
		handled = true
		assert.Equal(t, "value", headers.Get("X-Test"))
		assert.Equal(t, "payload", string(body))
		return nil
	}

	server.AddRoute(http.MethodPost, "/webhook", handler)
	require.True(t, server.alreadyRegistered)

	reqHeaders := http.Header{}
	reqHeaders.Set("X-Test", "value")
	require.NoError(t, server.handler(t.Context(), reqHeaders, []byte("payload")))
	assert.True(t, handled)
}

func TestStartAndStop(t *testing.T) {
	t.Parallel()

	server := NewFakeServer(t, http.MethodGet, "/health")

	go func() {
		assert.NoError(t, server.Start())
	}()

	<-server.StartedServer()
	require.NoError(t, server.Stop())
	<-server.StoppedServer()
}

func TestCallRegisterWebhook(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	server := NewFakeServer(t, http.MethodPost, "/webhook")

	handled := make(chan struct{}, 1)
	server.AddRoute(http.MethodPost, "/webhook", func(ctx context.Context, headers http.Header, body []byte) error {
		handled <- struct{}{}
		return nil
	})

	require.NoError(t, server.CallRegisterWebhook(ctx))

loop:
	for {
		select {
		case <-handled:
			break loop
		case <-ctx.Done():
			assert.Fail(t, "context cancelled", "error", ctx.Err())
			return
		}
	}
}

func TestStartAsyncSignalsStarted(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	server := NewFakeServer(t, http.MethodGet, "/health")
	server.StartAsync(ctx)

loop:
	for {
		select {
		case <-server.StartedServer():
			break loop
		case <-ctx.Done():
			assert.Fail(t, "context cancelled", "error", ctx.Err())
			return
		}
	}
}
