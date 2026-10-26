// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// serverLifecycleTimeout bounds how long a test waits for the server to
	// start serving and to shut down, so a stuck server fails the test quickly.
	serverLifecycleTimeout = 5 * time.Second
	// servingProbeInterval is the delay between two readiness probes.
	servingProbeInterval = 10 * time.Millisecond
	// servingProbeTimeout bounds a single readiness probe request.
	servingProbeTimeout = 200 * time.Millisecond
)

// newListeningTestServer returns a server bound to a random free port on the
// loopback interface, and a channel that receives the listening address once
// the server has opened its socket.
func newListeningTestServer(t *testing.T) (*impServer, <-chan string) {
	t.Helper()

	srv := &impServer{
		app: fiber.New(fiber.Config{DisableStartupMessage: true, Immutable: true}),
		config: config{
			HTTPHost: "127.0.0.1",
			HTTPPort: 0,
		},
	}

	addrChan := make(chan string, 1)
	srv.app.Hooks().OnListen(func(ln fiber.ListenData) error {
		t.Logf("Server is listening on %s:%s", ln.Host, ln.Port)
		addrChan <- net.JoinHostPort(ln.Host, ln.Port)
		return nil
	})

	// Release the socket even when the test fails before stopping the server.
	t.Cleanup(func() {
		assert.NoError(t, srv.app.ShutdownWithTimeout(serverLifecycleTimeout))
	})

	return srv, addrChan
}

// stopWhenServing stops the server once it is ready to accept requests.
//
// The OnListen hook runs before the listener is handed to the underlying
// fasthttp server, and a shutdown issued in that window finds no listener
// and silently does nothing, leaving the server running. Waiting for an
// HTTP response guarantees the listener is registered, so Stop takes effect.
func stopWhenServing(ctx context.Context, srv *impServer, addrChan <-chan string) error {
	var addr string
	select {
	case addr = <-addrChan:
	case <-ctx.Done():
		return fmt.Errorf("waiting for the server to listen: %w", ctx.Err())
	}

	if err := waitUntilServing(ctx, "http://"+addr+"/"); err != nil {
		return err
	}

	return srv.Stop(ctx)
}

// waitUntilServing polls url until the server sends back any HTTP response.
func waitUntilServing(ctx context.Context, url string) error {
	client := &http.Client{
		Timeout:   servingProbeTimeout,
		Transport: &http.Transport{DisableKeepAlives: true},
	}

	ticker := time.NewTicker(servingProbeInterval)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("building readiness probe: %w", err)
		}

		resp, err := client.Do(req)
		if err == nil {
			return resp.Body.Close()
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for the server to serve: %w (last probe error: %w)", ctx.Err(), err)
		case <-ticker.C:
		}
	}
}

// receiveWithin returns the next value sent on ch, failing the test if
// nothing arrives before ctx is done.
func receiveWithin[T any](ctx context.Context, t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s", what)
	}

	var zero T
	return zero
}

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
	ctx, cancel := context.WithTimeout(t.Context(), serverLifecycleTimeout)
	defer cancel()

	srv, addrChan := newListeningTestServer(t)

	stopErrChan := make(chan error, 1)
	go func() {
		stopErrChan <- stopWhenServing(ctx, srv, addrChan)
	}()

	startErrChan := make(chan error, 1)
	go func() {
		startErrChan <- srv.Start()
	}()

	require.NoError(t, receiveWithin(ctx, t, startErrChan, "Start to return"))
	require.NoError(t, receiveWithin(ctx, t, stopErrChan, "Stop to return"))
}

func TestStartAsyncServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), serverLifecycleTimeout)
	defer cancel()

	srv, addrChan := newListeningTestServer(t)

	stopErrChan := make(chan error, 1)
	go func() {
		stopErrChan <- stopWhenServing(ctx, srv, addrChan)
	}()

	errChan := srv.StartAsync()

	require.NoError(t, receiveWithin(ctx, t, errChan, "StartAsync to report the server exit"))
	require.NoError(t, receiveWithin(ctx, t, stopErrChan, "Stop to return"))
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
