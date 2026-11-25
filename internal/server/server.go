// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/sync/errgroup"

	"github.com/mia-platform/ibdm/internal/info"
	"github.com/mia-platform/ibdm/internal/logger"
)

const (
	serviceName = "ibdm"

	// webhookListenerName names the public listener that serves webhooks and status routes.
	webhookListenerName = "webhook"

	msgWebhookError   = "error processing webhook message"
	msgInternalError  = "internal server error"
	msgBodyTooLarge   = "request body too large"
	msgBodyUnreadable = "error reading request body"
)

// Server serves the webhooks of the sources together with the status routes.
type Server interface {
	// AddRoute registers handler for the requests with the given method and path. It fails when
	// the route is invalid or conflicts with a route already registered.
	AddRoute(method string, path string, handler func(ctx context.Context, headers http.Header, body []byte) error) error
	// Start listens and serves until the server is stopped or fails.
	Start() error
	// Stop shuts the server down gracefully, waiting for active requests until ctx is done.
	Stop(context.Context) error
	// StartAsync runs Start in a goroutine; the returned channel receives its result and is closed.
	StartAsync() <-chan error
}

var _ Server = &impServer{}

type impServer struct {
	config

	appCtx    context.Context
	webhook   *listener
	listeners []*listener
}

var (
	ErrServerListen   = errors.New("server listen error")
	ErrServerShutdown = errors.New("server shutdown error")
	ErrInvalidRoute   = errors.New("invalid route")
)

// NewServer returns a Server configured from the environment. ctx is the application context:
// webhook handlers receive a context derived from it, never one tied to the HTTP request.
func NewServer(ctx context.Context) (Server, error) {
	cfg, err := LoadServerConfig()
	if err != nil {
		return nil, err
	}

	return newServer(ctx, *cfg), nil
}

// newServer builds the server with its webhook listener and the status routes.
func newServer(ctx context.Context, cfg config) *impServer {
	log := logger.FromContext(ctx)

	webhook := newListener(
		webhookListenerName,
		net.JoinHostPort(cfg.HTTPHost, strconv.Itoa(cfg.HTTPPort)),
		log,
		logger.RequestMiddlewareLogger(log, []string{statusRoutesPrefix}),
		recoverPanic,
		normalizePath,
		limitBody(cfg.MaxBodySize),
	)
	statusRoutes(webhook.mux, serviceName, info.Version)

	return &impServer{
		config:    cfg,
		appCtx:    ctx,
		webhook:   webhook,
		listeners: []*listener{webhook},
	}
}

// AddRoute registers handler on the webhook listener. Paths match regardless of case and of a
// trailing slash. The handler is answered with 204 when it returns nil, and with 500 otherwise.
func (s *impServer) AddRoute(method string, path string, handler func(ctx context.Context, headers http.Header, body []byte) error) error {
	if err := validateRoute(method, path); err != nil {
		return fmt.Errorf("%w: %s %q: %w", ErrInvalidRoute, method, path, err)
	}

	if err := handle(s.webhook.mux, method+" "+routingPath(path), s.webhookHandler(handler)); err != nil {
		return fmt.Errorf("%w: %s %q: %w", ErrInvalidRoute, method, path, err)
	}
	return nil
}

// validateRoute rejects the routes that cannot be registered as a literal method and path.
// Braces are rejected because the router reads them as wildcards.
func validateRoute(method, path string) error {
	switch {
	case method == "" || strings.ContainsFunc(method, unicode.IsSpace):
		return errors.New("method must be a single word")
	case !strings.HasPrefix(path, "/"):
		return errors.New("path must start with '/'")
	case strings.ContainsAny(path, "{} \t\n\r"):
		return errors.New("path must not contain braces or whitespace")
	}
	return nil
}

// handle registers handler on mux for pattern, turning the panic raised by the router for an
// invalid or conflicting pattern into an error.
func handle(mux *http.ServeMux, pattern string, handler http.Handler) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%v", recovered)
		}
	}()

	mux.Handle(pattern, handler)
	return nil
}

// webhookHandler adapts handler to net/http. The handler receives a copy of the headers and the
// whole body, so it may keep using them after the response is sent, and a context derived from
// the application context carrying the request logger: work started by the handler is not
// cancelled when the response is sent.
func (s *impServer) webhookHandler(handler func(ctx context.Context, headers http.Header, body []byte) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := logger.FromContext(r.Context())

		body, err := io.ReadAll(r.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeError(w, log, http.StatusRequestEntityTooLarge, msgBodyTooLarge)
				return
			}
			writeError(w, log, http.StatusBadRequest, msgBodyUnreadable)
			return
		}

		// Sources start work from the handler that must go on after the response is sent, so the
		// context comes from the application, not from the request.
		ctx := logger.WithContext(s.appCtx, log)
		if err := handler(ctx, r.Header.Clone(), body); err != nil { //nolint:contextcheck // see above
			writeError(w, log, http.StatusInternalServerError, msgWebhookError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// Start opens every listener and serves them until they are stopped. When one listener fails, the
// others are closed and the error is returned. A stop is a normal end and returns nil.
func (s *impServer) Start() error {
	for i, l := range s.listeners {
		if err := l.listen(s.appCtx); err != nil {
			for _, opened := range s.listeners[:i] {
				err = errors.Join(err, opened.close())
			}
			return fmt.Errorf("%w: %w", ErrServerListen, err)
		}
	}

	log := logger.FromContext(s.appCtx).WithName(serverLoggerName)
	for _, l := range s.listeners {
		if s.DisableStartupMessage {
			log.Debug("server listening", "listener", l.name, "address", l.boundAddr())
			continue
		}
		log.Info("server listening", "listener", l.name, "address", l.boundAddr())
	}

	var group errgroup.Group
	for _, l := range s.listeners {
		group.Go(func() error {
			err := l.serve()
			if err != nil {
				for _, other := range s.listeners {
					if other != l {
						err = errors.Join(err, other.close())
					}
				}
			}
			return err
		})
	}

	if err := group.Wait(); err != nil {
		return fmt.Errorf("%w: %w", ErrServerListen, err)
	}
	return nil
}

// Stop shuts every listener down gracefully. Stopping a server that has not started yet makes a
// later Start return immediately.
func (s *impServer) Stop(ctx context.Context) error {
	var errs []error
	for _, l := range s.listeners {
		if err := l.shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: %w", ErrServerShutdown, err)
	}
	return nil
}

func (s *impServer) StartAsync() <-chan error {
	errorChan := make(chan error)
	go func() {
		err := s.Start()
		errorChan <- err
		close(errorChan)
	}()

	return errorChan
}
