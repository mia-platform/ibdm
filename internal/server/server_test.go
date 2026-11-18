// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/logger"
)

const (
	// serverLifecycleTimeout bounds how long a test waits for the server to
	// start serving and to shut down, so a stuck server fails the test quickly.
	serverLifecycleTimeout = 5 * time.Second

	webhookErrorBody = `{"error":"Internal Server Error","message":"error processing webhook message","statusCode":500}`

	// testMaxBodySize is the default of HTTP_MAX_BODY_SIZE.
	testMaxBodySize = 4 * 1024 * 1024
)

// syncBuffer is a bytes.Buffer safe for concurrent use: the server writes logs from its own
// goroutines while the test reads them.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// testConfig returns the default configuration of the server, bound to a random free port on
// the loopback interface.
func testConfig() config {
	return config{
		DisableStartupMessage: true,
		HTTPHost:              "127.0.0.1",
		HTTPPort:              0,
		MaxBodySize:           testMaxBodySize,
	}
}

// newTestServer returns a server built from testConfig, whose logs are written to the returned
// buffer.
func newTestServer(t *testing.T) (*impServer, *syncBuffer) {
	t.Helper()
	return newTestServerWithConfig(t, testConfig())
}

// newTestServerWithConfig returns a server built from cfg, whose logs are written to the
// returned buffer.
func newTestServerWithConfig(t *testing.T, cfg config) (*impServer, *syncBuffer) {
	t.Helper()

	buffer := new(syncBuffer)
	ctx := logger.WithContext(t.Context(), logger.NewLogger(buffer))
	return newServer(ctx, cfg), buffer
}

// startTestServer starts srv and returns the address of its webhook listener once it accepts
// connections, together with the channel receiving the result of Start.
func startTestServer(t *testing.T, srv *impServer) (string, <-chan error) {
	t.Helper()

	errChan := srv.StartAsync()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), serverLifecycleTimeout)
		defer cancel()
		assert.NoError(t, srv.Stop(ctx))
	})

	waitListening(t, srv.webhook)
	return srv.webhook.boundAddr(), errChan
}

// waitListening waits until l has opened its socket.
func waitListening(t *testing.T, l *listener) {
	t.Helper()

	select {
	case <-l.ready:
	case <-time.After(serverLifecycleTimeout):
		t.Fatalf("timed out waiting for listener %s to listen", l.name)
	}
}

// receiveWithin returns the next value sent on ch, failing the test if
// nothing arrives before the lifecycle timeout.
func receiveWithin[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(serverLifecycleTimeout):
		t.Fatalf("timed out waiting for %s", what)
	}

	var zero T
	return zero
}

// getStatusCode sends a GET to url and returns the status code of the response.
func getStatusCode(t *testing.T, url string) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	response, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	return response.StatusCode
}

// serve sends req to the handler chain of the webhook listener of srv.
func serve(srv *impServer, req *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	srv.webhook.srv.Handler.ServeHTTP(recorder, req)
	return recorder
}

func TestNewServer(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		srvInterface, err := NewServer(t.Context())
		require.NoError(t, err)

		srv, ok := srvInterface.(*impServer)
		require.True(t, ok)
		require.Equal(t, "0.0.0.0:3000", srv.webhook.addr)
		require.Equal(t, []*listener{srv.webhook}, srv.listeners)

		response := serve(srv, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/-/healthz", nil))
		require.Equal(t, http.StatusOK, response.Code)
	})

	t.Run("host and port from the environment", func(t *testing.T) {
		t.Setenv("HTTP_HOST", "127.0.0.1")
		t.Setenv("HTTP_PORT", "8080")

		srvInterface, err := NewServer(t.Context())
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1:8080", srvInterface.(*impServer).webhook.addr)
	})

	t.Run("invalid environment", func(t *testing.T) {
		t.Setenv("HTTP_PORT", "655350")

		_, err := NewServer(t.Context())
		require.ErrorIs(t, err, ErrEnvVariablesNotValid)
	})
}

func TestAddRoute(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		method         string
		target         string
		body           string
		handlerErr     error
		expectedCode   int
		expectedBody   string
		expectedCalled bool
	}{
		"handler success": {
			method:         http.MethodPost,
			target:         "/github/webhook",
			body:           "payload",
			expectedCode:   http.StatusNoContent,
			expectedCalled: true,
		},
		"handler error": {
			method:         http.MethodPost,
			target:         "/github/webhook",
			body:           "payload",
			handlerErr:     assert.AnError,
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   webhookErrorBody,
			expectedCalled: true,
		},
		"path matches regardless of case": {
			method:         http.MethodPost,
			target:         "/GitHub/WebHook",
			body:           "payload",
			expectedCode:   http.StatusNoContent,
			expectedCalled: true,
		},
		"path matches with a trailing slash": {
			method:         http.MethodPost,
			target:         "/github/webhook/",
			body:           "payload",
			expectedCode:   http.StatusNoContent,
			expectedCalled: true,
		},
		"query string is ignored": {
			method:         http.MethodPost,
			target:         "/github/webhook?token=fictional-secret-value",
			body:           "payload",
			expectedCode:   http.StatusNoContent,
			expectedCalled: true,
		},
		"unknown path": {
			method:       http.MethodPost,
			target:       "/gitlab/webhook",
			expectedCode: http.StatusNotFound,
		},
		"wrong method": {
			method:       http.MethodGet,
			target:       "/github/webhook",
			expectedCode: http.StatusMethodNotAllowed,
		},
		"body at the limit": {
			method:         http.MethodPost,
			target:         "/github/webhook",
			body:           strings.Repeat("a", testMaxBodySize),
			expectedCode:   http.StatusNoContent,
			expectedCalled: true,
		},
		"body over the limit": {
			method:       http.MethodPost,
			target:       "/github/webhook",
			body:         strings.Repeat("a", testMaxBodySize+1),
			expectedCode: http.StatusRequestEntityTooLarge,
			expectedBody: `{"error":"Request Entity Too Large","message":"request body too large","statusCode":413}`,
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			srv, _ := newTestServer(t)
			called := false
			srv.AddRoute(http.MethodPost, "/github/webhook", func(_ context.Context, _ http.Header, body []byte) error {
				called = true
				assert.Equal(t, test.body, string(body))
				return test.handlerErr
			})

			req := httptest.NewRequestWithContext(t.Context(), test.method, test.target, strings.NewReader(test.body))
			response := serve(srv, req)

			require.Equal(t, test.expectedCode, response.Code)
			require.Equal(t, test.expectedCalled, called)
			if test.expectedBody != "" {
				require.Equal(t, test.expectedBody, response.Body.String())
				require.Equal(t, "application/json", response.Header().Get("Content-Type"))
			}
			if test.expectedCode == http.StatusMethodNotAllowed {
				require.Equal(t, http.MethodPost, response.Header().Get("Allow"))
			}
		})
	}
}

func TestAddRouteRegisteredWithMixedCase(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)
	called := false
	srv.AddRoute(http.MethodPost, "/My-Org/Webhook/", func(context.Context, http.Header, []byte) error {
		called = true
		return nil
	})

	response := serve(srv, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/my-org/webhook", nil))
	require.Equal(t, http.StatusNoContent, response.Code)
	require.True(t, called)
}

func TestWebhookHandlerReceivesHeaderCopy(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)
	var received http.Header
	srv.AddRoute(http.MethodPost, "/webhook", func(_ context.Context, headers http.Header, _ []byte) error {
		received = headers
		return nil
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/webhook", nil)
	req.Header.Set("X-Event", "push")
	require.Equal(t, http.StatusNoContent, serve(srv, req).Code)

	req.Header.Set("X-Event", "changed after the response")
	require.Equal(t, "push", received.Get("X-Event"))
}

func TestWebhookHandlerReadError(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)
	called := false
	srv.AddRoute(http.MethodPost, "/webhook", func(context.Context, http.Header, []byte) error {
		called = true
		return nil
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/webhook", io.NopCloser(failingReader{}))
	response := serve(srv, req)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.JSONEq(t, `{"error":"Bad Request","message":"error reading request body","statusCode":400}`, response.Body.String())
	require.False(t, called)
}

// failingReader is a reader that always fails.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, assert.AnError }

func TestWebhookHandlerContextOutlivesRequest(t *testing.T) {
	t.Parallel()

	srv, buffer := newTestServer(t)
	type ctxCheck struct {
		err    error
		logger logger.Logger
	}
	release := make(chan struct{})
	checked := make(chan ctxCheck, 1)
	srv.AddRoute(http.MethodPost, "/webhook", func(ctx context.Context, _ http.Header, _ []byte) error {
		go func() {
			<-release
			checked <- ctxCheck{err: ctx.Err(), logger: logger.FromContext(ctx)}
		}()
		return nil
	})

	addr, _ := startTestServer(t, srv)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+addr+"/webhook", strings.NewReader("payload"))
	require.NoError(t, err)
	req.Header.Set("X-Request-Id", "019f1cb8-0000-7000-8000-000000000003")
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	response, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusNoContent, response.StatusCode)

	// The request is over on both sides: work started by the handler must still run.
	close(release)
	result := receiveWithin(t, checked, "the handler goroutine")
	require.NoError(t, result.err)

	result.logger.WithName("ibdm:source:fake").Info("logged from the handler")
	var handlerLine map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buffer.String()), "\n") {
		if strings.Contains(line, "logged from the handler") {
			require.NoError(t, json.Unmarshal([]byte(line), &handlerLine))
		}
	}
	require.Equal(t, "ibdm:source:fake", handlerLine["@module"])
	require.Equal(t, "019f1cb8-0000-7000-8000-000000000003", handlerLine["requestId"])
}

func TestServerLifecycle(t *testing.T) {
	t.Parallel()

	t.Run("start and stop", func(t *testing.T) {
		t.Parallel()

		srv, _ := newTestServer(t)
		addr, errChan := startTestServer(t, srv)

		require.Equal(t, http.StatusOK, getStatusCode(t, "http://"+addr+"/-/ready"))

		require.NoError(t, srv.Stop(t.Context()))
		require.NoError(t, receiveWithin(t, errChan, "Start to return"))

		_, err := net.Dial("tcp", addr)
		require.Error(t, err)
	})

	t.Run("stop before start", func(t *testing.T) {
		t.Parallel()

		srv, _ := newTestServer(t)
		require.NoError(t, srv.Stop(t.Context()))
		require.NoError(t, receiveWithin(t, srv.StartAsync(), "Start to return"))
	})

	t.Run("address in use", func(t *testing.T) {
		t.Parallel()

		var lc net.ListenConfig
		busy, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer busy.Close()

		srv, _ := newTestServer(t)
		srv.webhook.addr = busy.Addr().String()
		require.ErrorIs(t, srv.Start(), ErrServerListen)
	})
}

func TestServerWithTwoListeners(t *testing.T) {
	t.Parallel()

	newTwoListenersServer := func(t *testing.T) (*impServer, *listener) {
		t.Helper()

		srv, _ := newTestServer(t)
		second := newListener("control", "127.0.0.1:0", logger.NewLogger(io.Discard))
		second.mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		srv.listeners = append(srv.listeners, second)
		return srv, second
	}

	t.Run("both serve and stop together", func(t *testing.T) {
		t.Parallel()

		srv, second := newTwoListenersServer(t)
		_, errChan := startTestServer(t, srv)
		waitListening(t, second)

		require.Equal(t, http.StatusOK, getStatusCode(t, "http://"+second.boundAddr()+"/status"))

		require.NoError(t, srv.Stop(t.Context()))
		require.NoError(t, receiveWithin(t, errChan, "Start to return"))
	})

	t.Run("a failing listener closes the other", func(t *testing.T) {
		t.Parallel()

		srv, second := newTwoListenersServer(t)
		addr, errChan := startTestServer(t, srv)
		waitListening(t, second)

		// Closing the socket under the second listener makes its serve loop fail.
		second.mu.Lock()
		require.NoError(t, second.ln.Close())
		second.mu.Unlock()

		require.ErrorIs(t, receiveWithin(t, errChan, "Start to return"), ErrServerListen)
		_, err := net.Dial("tcp", addr)
		require.Error(t, err)
	})

	t.Run("a listener failing to listen releases the ones already open", func(t *testing.T) {
		t.Parallel()

		srv, second := newTwoListenersServer(t)
		second.addr = "127.0.0.1:-1"

		require.ErrorIs(t, srv.Start(), ErrServerListen)

		first := srv.webhook.boundAddr()
		require.NotEmpty(t, first)
		_, err := net.Dial("tcp", first)
		require.Error(t, err)
	})
}

func TestRoutingPath(t *testing.T) {
	t.Parallel()

	for path, expected := range map[string]string{
		"/":                "/",
		"//":               "/",
		"/github/webhook":  "/github/webhook",
		"/GitHub/Webhook/": "/github/webhook",
		"/a//":             "/a",
	} {
		require.Equal(t, expected, routingPath(path), path)
	}
}

func TestChainOrder(t *testing.T) {
	t.Parallel()

	var order []string
	mark := func(name string) middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	handler := chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order = append(order, "handler")
	}), mark("outer"), mark("inner"))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	require.Equal(t, []string{"outer", "inner", "handler"}, order)
}

func TestServerHardening(t *testing.T) {
	t.Parallel()

	t.Run("timeouts", func(t *testing.T) {
		t.Parallel()

		srv, _ := newTestServer(t)
		require.Equal(t, readHeaderTimeout, srv.webhook.srv.ReadHeaderTimeout)
		require.Equal(t, readTimeout, srv.webhook.srv.ReadTimeout)
		require.Equal(t, idleTimeout, srv.webhook.srv.IdleTimeout)
		require.Zero(t, srv.webhook.srv.WriteTimeout, "handlers may wait on the destination")
	})

	t.Run("body limit from the configuration", func(t *testing.T) {
		t.Parallel()

		cfg := testConfig()
		cfg.MaxBodySize = 10
		srv, _ := newTestServerWithConfig(t, cfg)
		srv.AddRoute(http.MethodPost, "/webhook", func(context.Context, http.Header, []byte) error { return nil })

		ok := serve(srv, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/webhook", strings.NewReader(strings.Repeat("a", 10))))
		require.Equal(t, http.StatusNoContent, ok.Code)
		tooLarge := serve(srv, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/webhook", strings.NewReader(strings.Repeat("a", 11))))
		require.Equal(t, http.StatusRequestEntityTooLarge, tooLarge.Code)
	})

	t.Run("a panicking handler is answered 500 and the server keeps serving", func(t *testing.T) {
		t.Parallel()

		srv, logs := newTestServer(t)
		srv.AddRoute(http.MethodPost, "/panic", func(context.Context, http.Header, []byte) error {
			panic("fictional failure")
		})

		response := serve(srv, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/panic", nil))
		require.Equal(t, http.StatusInternalServerError, response.Code)
		require.JSONEq(t, `{"error":"Internal Server Error","message":"internal server error","statusCode":500}`, response.Body.String())
		require.Contains(t, logs.String(), "panic serving request")
		require.Contains(t, logs.String(), "fictional failure")

		healthy := serve(srv, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/-/healthz", nil))
		require.Equal(t, http.StatusOK, healthy.Code)
	})

	t.Run("an aborted handler is not recovered", func(t *testing.T) {
		t.Parallel()

		handler := recoverPanic(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		}))

		require.PanicsWithValue(t, http.ErrAbortHandler, func() {
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		})
	})

	t.Run("errors of the http server are logged", func(t *testing.T) {
		t.Parallel()

		srv, logs := newTestServer(t)
		srv.webhook.srv.ErrorLog.Print("http: TLS handshake error from 192.0.2.10:443: EOF\n")

		require.Contains(t, logs.String(), "http server error")
		require.Contains(t, logs.String(), "TLS handshake error from 192.0.2.10:443: EOF")
		require.Contains(t, logs.String(), serverLoggerName)
	})
}

func TestStartupMessage(t *testing.T) {
	t.Parallel()

	for testName, disabled := range map[string]bool{
		"enabled":  false,
		"disabled": true,
	} {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig()
			cfg.DisableStartupMessage = disabled
			srv, logs := newTestServerWithConfig(t, cfg)
			addr, _ := startTestServer(t, srv)

			// The message is logged right after every listener opens its socket.
			require.Equal(t, http.StatusOK, getStatusCode(t, "http://"+addr+"/-/healthz"))
			if disabled {
				require.NotContains(t, logs.String(), "server listening")
				return
			}
			require.Contains(t, logs.String(), "server listening")
			require.Contains(t, logs.String(), addr)
		})
	}
}
