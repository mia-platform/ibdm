// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mia-platform/ibdm/internal/logger"
)

const (
	// readHeaderTimeout bounds the time a client has to send the request headers.
	readHeaderTimeout = 10 * time.Second
	// readTimeout bounds the time a client has to send the whole request, body included.
	readTimeout = 60 * time.Second
	// idleTimeout bounds how long a keep-alive connection waits for the next request.
	// There is no write timeout: some webhook handlers answer only after their data has been
	// mapped and sent to the destination, which can take longer than any fixed bound.
	idleTimeout = 120 * time.Second

	serverLoggerName = "ibdm:server"
)

// errorLogWriter forwards the errors net/http reports about connections (TLS handshakes,
// malformed requests, recovered panics) to the IBDM logger.
type errorLogWriter struct {
	log logger.Logger
}

func (w errorLogWriter) Write(p []byte) (int, error) {
	w.log.Error("http server error", "error", strings.TrimSpace(string(p)))
	return len(p), nil
}

// listener is one address the server listens on, with its own routes and middleware chain.
type listener struct {
	name string
	addr string
	mux  *http.ServeMux
	srv  *http.Server

	mu    sync.Mutex
	ln    net.Listener
	ready chan struct{}
}

// newListener returns a listener for addr whose routes, registered on its mux, are served through
// mws: mws[0] is the outermost middleware. Errors of the underlying server are logged on appLog.
func newListener(name, addr string, appLog logger.Logger, mws ...middleware) *listener {
	mux := http.NewServeMux()
	return &listener{
		name: name,
		addr: addr,
		mux:  mux,
		srv: &http.Server{
			Handler:           chain(mux, mws...),
			ReadHeaderTimeout: readHeaderTimeout,
			ReadTimeout:       readTimeout,
			IdleTimeout:       idleTimeout,
			ErrorLog:          log.New(errorLogWriter{log: appLog.WithName(serverLoggerName)}, "", 0),
		},
		ready: make(chan struct{}),
	}
}

// listen opens the socket of the listener.
func (l *listener) listen(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", l.addr)
	if err != nil {
		return fmt.Errorf("listener %s on %s: %w", l.name, l.addr, err)
	}

	l.mu.Lock()
	l.ln = ln
	l.mu.Unlock()
	close(l.ready)
	return nil
}

// serve accepts connections on the socket opened by listen until the listener is shut down.
// A shutdown is a normal end and returns nil.
func (l *listener) serve() error {
	l.mu.Lock()
	ln := l.ln
	l.mu.Unlock()

	if err := l.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listener %s on %s: %w", l.name, l.addr, err)
	}
	return nil
}

// shutdown stops the listener gracefully, waiting for active requests until ctx is done.
func (l *listener) shutdown(ctx context.Context) error {
	if err := l.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("listener %s: %w", l.name, err)
	}
	return nil
}

// close stops the listener immediately, dropping active connections. When the socket was opened
// but serve never ran, it is released here.
func (l *listener) close() error {
	err := l.srv.Close()

	l.mu.Lock()
	ln := l.ln
	l.mu.Unlock()
	if ln != nil {
		if closeErr := ln.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			err = errors.Join(err, closeErr)
		}
	}

	if err != nil {
		return fmt.Errorf("listener %s: %w", l.name, err)
	}
	return nil
}

// boundAddr returns the address the socket is bound to, which differs from addr when addr asks
// for a random port. It is empty until listen succeeds.
func (l *listener) boundAddr() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.ln == nil {
		return ""
	}
	return l.ln.Addr().String()
}
