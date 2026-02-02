// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewApp(t *testing.T) {
	t.Run("successfully creates app with valid config", func(t *testing.T) {
		ctx := t.Context()
		t.Setenv("HTTP_PORT", "3000")

		srv, err := NewServer(ctx)
		require.NoError(t, err)
		require.NotNil(t, srv)

		require.NotNil(t, srv.app)

		time.Sleep(1 * time.Second)
		request := httptest.NewRequest(http.MethodGet, "/-/healthz", nil)
		response, err := srv.app.Test(request)
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
		t.Setenv("HTTP_PORT", "3003")

		srv, err := NewServer(t.Context())
		require.NoError(t, err)

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
	})
}
