// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestNewApp(t *testing.T) {
	t.Run("successfully creates app with valid config", func(t *testing.T) {
		ctx := t.Context()
		t.Setenv("HTTP_PORT", "3000")
		t.Setenv("LOG_LEVEL", "INFO")

		srv, err := NewServer(ctx)
		require.NoError(t, err)
		require.NotNil(t, srv)

		app := srv.App()
		require.NotNil(t, app)

		time.Sleep(1 * time.Second)
		request := httptest.NewRequest(http.MethodGet, "/-/healthz", nil)
		response, err := app.Test(request)
		require.NoError(t, err)

		defer response.Body.Close()
		require.Equal(t, http.StatusOK, response.StatusCode)
		defer srv.app.Shutdown()
	})
}

func TestStartServer(t *testing.T) {
	t.Run("starts and stops the server successfully", func(t *testing.T) {
		ctx := t.Context()
		t.Setenv("HTTP_PORT", "3001")
		t.Setenv("LOG_LEVEL", "INFO")

		srv, err := NewServer(ctx)
		require.NoError(t, err)
		require.NotNil(t, srv)

		errChan := make(chan error, 1)
		go func() {
			err := srv.Start()
			errChan <- err
		}()

		time.Sleep(1 * time.Second)
		request := httptest.NewRequest(http.MethodGet, "/-/healthz", nil)
		response, err := srv.app.Test(request)
		require.NoError(t, err)

		defer response.Body.Close()
		require.Equal(t, http.StatusOK, response.StatusCode)

		err = srv.app.Shutdown()
		require.NoError(t, err)
		err = <-errChan
		require.NoError(t, err)
	})
}

func TestStartAsyncServer(t *testing.T) {
	t.Run("starts the server asynchronously", func(t *testing.T) {
		ctx := t.Context()
		t.Setenv("HTTP_PORT", "3002")
		t.Setenv("LOG_LEVEL", "INFO")

		srv, err := NewServer(ctx)
		require.NoError(t, err)
		require.NotNil(t, srv)

		srv.StartAsync(ctx)

		time.Sleep(1 * time.Second)
		request := httptest.NewRequest(http.MethodGet, "/-/healthz", nil)
		response, err := srv.app.Test(request)
		require.NoError(t, err)

		defer response.Body.Close()
		require.Equal(t, http.StatusOK, response.StatusCode)

		err = srv.app.Shutdown()
		require.NoError(t, err)
	})
}

func TestFiberHandlerWrapper(t *testing.T) {
	t.Run("wraps handler and processes request body", func(t *testing.T) {
		t.Setenv("LOG_LEVEL", "INFO")

		processed := false
		handler := func(body []byte) error {
			processed = true
			require.Equal(t, "test body", string(body))
			return nil
		}

		fiberHandler := FiberHandlerWrapper(handler)

		app := fiber.New()
		app.Post("/test", fiberHandler)

		request := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("test body"))

		response, err := app.Test(request)
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, response.StatusCode)
		require.True(t, processed)

		defer response.Body.Close()
	})
}
