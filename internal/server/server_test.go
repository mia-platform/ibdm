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
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	t.Parallel()
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
			HTTPPort: 0,
		},
	}

	syncChan := make(chan struct{})
	srv.app.Hooks().OnListen(func(ln fiber.ListenData) error {
		t.Logf("Server is listening on %s:%s", ln.Host, ln.Port)
		close(syncChan)
		return nil
	})

	errChan := make(chan error)
	defer close(errChan)
	go func() {
		err := srv.Start()
		errChan <- err
	}()

	<-syncChan
	require.NoError(t, srv.Stop())
	err := <-errChan
	require.NoError(t, err)
}

func TestStartAsyncServer(t *testing.T) {
	srv := &impServer{
		app: fiber.New(fiber.Config{DisableStartupMessage: true, Immutable: true}),
		config: config{
			HTTPHost: "127.0.0.1",
			HTTPPort: 0,
		},
	}

	syncChan := make(chan struct{})
	srv.app.Hooks().OnListen(func(ln fiber.ListenData) error {
		t.Logf("Server is listening on %s:%s", ln.Host, ln.Port)
		close(syncChan)
		return nil
	})

	srv.StartAsync(t.Context())
	<-syncChan
	require.NoError(t, srv.Stop())
}

func TestFiberHandlerWrapper(t *testing.T) {
	t.Parallel()
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
