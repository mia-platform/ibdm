// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	ctx := t.Context()

	srvInterface, err := NewServer(ctx)
	srv, ok := srvInterface.(*impServer)
	require.True(t, ok)

	require.NoError(t, err)
	require.NotNil(t, srv)

	require.NotNil(t, srv.app)
	require.True(t, srv.app.Config().Immutable)

	request := httptest.NewRequest(http.MethodGet, "/-/healthz", nil)
	response, err := srv.app.Test(request)
	require.NoError(t, err)

	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
}

func TestStartServer(t *testing.T) {
	srv := &impServer{
		app: fiber.New(fiber.Config{DisableStartupMessage: true, Immutable: true}),
		config: config{
			HTTPHost: "127.0.0.1",
			HTTPPort: 3001,
		},
	}

	syncChan := make(chan struct{})
	srv.app.Hooks().OnListen(func(ln fiber.ListenData) error {
		t.Logf("Server is listening on %s:%s", ln.Host, ln.Port)
		close(syncChan)
		return nil
	})

	stoppedChan := make(chan struct{})
	go func() {
		<-syncChan
		assert.NoError(t, srv.Stop(t.Context()))
		close(stoppedChan)
	}()

	err := srv.Start()
	assert.NoError(t, err)
	<-stoppedChan
}

func TestStartAsyncServer(t *testing.T) {
	srv := &impServer{
		app: fiber.New(fiber.Config{DisableStartupMessage: true, Immutable: true}),
		config: config{
			HTTPHost: "127.0.0.1",
			HTTPPort: 3002,
		},
	}

	syncChan := make(chan struct{})
	srv.app.Hooks().OnListen(func(ln fiber.ListenData) error {
		t.Logf("Server is listening on %s:%s", ln.Host, ln.Port)
		close(syncChan)
		return nil
	})

	stoppedChan := make(chan struct{})
	go func() {
		<-syncChan
		assert.NoError(t, srv.Stop(t.Context()))
		close(stoppedChan)
	}()

	errChan := srv.StartAsync()
	err := <-errChan
	require.NoError(t, err)
	<-stoppedChan
}

func TestFiberHandlerWrapper(t *testing.T) {
	srv := &impServer{
		app: fiber.New(fiber.Config{DisableStartupMessage: true, Immutable: true}),
	}

	processed := false
	handler := func(_ context.Context, headers http.Header, body []byte) error {
		processed = true
		require.Equal(t, "test body", string(body))
		return nil
	}

	request := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("test body"))

	srv.AddRoute(http.MethodPost, "/test", handler)
	response, err := srv.app.Test(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, response.StatusCode)
	require.True(t, processed)

	defer response.Body.Close()
}
