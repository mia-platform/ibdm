// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package logger

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestMiddlewareLogger(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		excludedPrefixes []string
		expectedLogLines int
		loggerLevel      Level
		path             string
		query            string
		method           string
		userAgent        string
		host             string
		statusCode       float64
	}{
		"logs incoming and completed requests": {
			excludedPrefixes: []string{},
			expectedLogLines: 2,
			loggerLevel:      TRACE,
			path:             "/foo",
			method:           http.MethodGet,
			userAgent:        "test-agent",
			host:             "example.com",
			statusCode:       http.StatusOK,
		},
		"skips excluded paths": {
			excludedPrefixes: []string{"/health"},
			expectedLogLines: 0,
			loggerLevel:      TRACE,
			path:             "/health",
			method:           http.MethodGet,
			userAgent:        "test-agent",
			host:             "example.com",
			statusCode:       http.StatusOK,
		},
		"skips incoming request log due to logger level": {
			excludedPrefixes: []string{},
			expectedLogLines: 1,
			loggerLevel:      INFO,
			path:             "/foo",
			method:           http.MethodGet,
			userAgent:        "test-agent",
			host:             "example.com",
			statusCode:       http.StatusOK,
		},
		"skips all logs due to excluded path and logger level": {
			excludedPrefixes: []string{"/foo"},
			expectedLogLines: 0,
			loggerLevel:      INFO,
			path:             "/foo",
			method:           http.MethodGet,
			userAgent:        "test-agent",
			host:             "example.com",
			statusCode:       http.StatusOK,
		},
		"the query string is never logged": {
			excludedPrefixes: []string{},
			expectedLogLines: 2,
			loggerLevel:      TRACE,
			path:             "/webhook",
			query:            "token=fictional-secret-value&page=2",
			method:           http.MethodGet,
			userAgent:        "test-agent",
			host:             "example.com",
			statusCode:       http.StatusOK,
		},
		"method POST logs incoming and completed requests": {
			excludedPrefixes: []string{},
			expectedLogLines: 2,
			loggerLevel:      TRACE,
			path:             "/bar",
			method:           http.MethodPost,
			userAgent:        "test-agent",
			host:             "example.com",
			statusCode:       http.StatusOK,
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			buffer := new(bytes.Buffer)
			logger := NewLogger(buffer)
			logger.SetLevel(test.loggerLevel)

			handler := RequestMiddlewareLogger(logger, test.excludedPrefixes)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("foo"))
			}))

			target := test.path
			if test.query != "" {
				target += "?" + test.query
			}
			req := httptest.NewRequestWithContext(t.Context(), test.method, target, nil)
			req.Header.Set("User-Agent", test.userAgent)
			req.Host = test.host
			handler.ServeHTTP(httptest.NewRecorder(), req)

			logs := strings.TrimSpace(buffer.String())
			lines := strings.Split(logs, "\n")
			if test.expectedLogLines == 0 {
				require.Empty(t, logs)
			} else {
				require.NotEmpty(t, logs)
				require.Len(t, lines, test.expectedLogLines, "Expected %d lines of logs", test.expectedLogLines)
			}

			if test.expectedLogLines == 0 {
				return
			}
			if test.query != "" {
				require.NotContains(t, logs, test.query, "no log line may carry the query string")
			}

			var firstLog map[string]any
			err := json.Unmarshal([]byte(lines[0]), &firstLog)
			require.NoError(t, err)

			httpReq := map[string]any{
				"method": test.method,
				"userAgent": map[string]any{
					"original": test.userAgent,
				},
			}
			url := map[string]any{
				"path": test.path,
			}
			host := map[string]any{
				"hostname": test.host,
			}

			require.NotNil(t, firstLog["http"])
			require.NotNil(t, firstLog["url"])
			require.Equal(t, url, firstLog["url"])
			require.NotNil(t, firstLog["host"])
			require.Equal(t, host, firstLog["host"])

			if test.loggerLevel == INFO {
				require.InEpsilon(t, test.statusCode, firstLog["http"].(map[string]any)["response"].(map[string]any)["statusCode"], 0.001)
				require.Equal(t, RequestCompletedMessage, firstLog["@message"])
				require.Equal(t, "info", strings.ToLower(firstLog["@level"].(string)))
				return
			}

			require.Equal(t, httpReq, firstLog["http"].(map[string]any)["request"])
			require.Equal(t, IncomingRequestMessage, firstLog["@message"])
			require.Equal(t, "trace", strings.ToLower(firstLog["@level"].(string)))

			var secondLog map[string]any
			err = json.Unmarshal([]byte(lines[1]), &secondLog)
			require.NoError(t, err)

			require.Equal(t, RequestCompletedMessage, secondLog["@message"])
			require.Equal(t, "info", strings.ToLower(secondLog["@level"].(string)))
			require.NotNil(t, secondLog["responseTime"])
		})
	}
}

// completedLog serves one request through the middleware and returns the decoded
// "request completed" log line.
func completedLog(t *testing.T, req *http.Request, handler http.HandlerFunc) map[string]any {
	t.Helper()

	buffer := new(bytes.Buffer)
	logger := NewLogger(buffer)
	RequestMiddlewareLogger(logger, nil)(handler).ServeHTTP(httptest.NewRecorder(), req)

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	require.Len(t, lines, 1)

	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &line))
	require.Equal(t, RequestCompletedMessage, line["@message"])
	return line
}

func TestRequestMiddlewareLoggerResponse(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		handler       http.HandlerFunc
		expectedCode  float64
		expectedBytes float64
	}{
		"body without explicit status is a 200": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("foo"))
			},
			expectedCode:  http.StatusOK,
			expectedBytes: 3,
		},
		"nothing written is a 200 without body": {
			handler:      func(http.ResponseWriter, *http.Request) {},
			expectedCode: http.StatusOK,
		},
		"no content": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
			expectedCode: http.StatusNoContent,
		},
		"server error with body": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
			},
			expectedCode:  http.StatusInternalServerError,
			expectedBytes: 16,
		},
		"not found": {
			handler:       http.NotFound,
			expectedCode:  http.StatusNotFound,
			expectedBytes: float64(len("404 page not found\n")),
		},
		"informational response is not the final status": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusEarlyHints)
				w.WriteHeader(http.StatusAccepted)
			},
			expectedCode: http.StatusAccepted,
		},
		"only the first final status counts": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectedCode: http.StatusCreated,
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/webhook", nil)
			line := completedLog(t, req, test.handler)

			response := line["http"].(map[string]any)["response"].(map[string]any)
			require.InEpsilon(t, test.expectedCode, response["statusCode"], 0.001)
			body := response["body"].(map[string]any)
			if test.expectedBytes == 0 {
				require.NotContains(t, body, "bytes")
				return
			}
			require.InEpsilon(t, test.expectedBytes, body["bytes"], 0.001)
		})
	}
}

func TestRequestMiddlewareLoggerRequestFields(t *testing.T) {
	t.Parallel()

	t.Run("request id from the caller names the logger", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/foo", nil)
		req.Header.Set("X-Request-Id", "019f1cb8-0000-7000-8000-000000000001")
		line := completedLog(t, req, func(http.ResponseWriter, *http.Request) {})

		require.Equal(t, "server:request:019f1cb8-0000-7000-8000-000000000001", line["@module"])
	})

	t.Run("generated request id when the caller sends none", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/foo", nil)
		line := completedLog(t, req, func(http.ResponseWriter, *http.Request) {})

		module, ok := line["@module"].(string)
		require.True(t, ok)
		require.True(t, strings.HasPrefix(module, "server:request:"))
		require.Len(t, strings.TrimPrefix(module, "server:request:"), len("019f1cb8-0000-7000-8000-000000000001"))
	})

	t.Run("host without port and forwarded headers", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/foo", nil)
		req.Host = "ibdm.example.com:3000"
		req.Header.Set("X-Forwarded-Host", "public.example.com")
		req.Header.Set("X-Forwarded-For", "192.0.2.10")
		line := completedLog(t, req, func(http.ResponseWriter, *http.Request) {})

		require.Equal(t, map[string]any{
			"hostname":      "ibdm.example.com",
			"forwardedHost": "public.example.com",
			"ip":            "192.0.2.10",
		}, line["host"])
	})

	t.Run("escaped path is logged as received", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/my%20org/webhook", nil)
		line := completedLog(t, req, func(http.ResponseWriter, *http.Request) {})

		require.Equal(t, map[string]any{"path": "/my%20org/webhook"}, line["url"])
	})
}

func TestRequestMiddlewareLoggerContext(t *testing.T) {
	t.Parallel()

	t.Run("logged request carries the logger of the request", func(t *testing.T) {
		t.Parallel()

		buffer := new(bytes.Buffer)
		handler := RequestMiddlewareLogger(NewLogger(buffer), nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			FromContext(r.Context()).Info("logged by the handler")
			FromContext(r.Context()).WithName("ibdm:source:fake").Info("logged by a source")
		}))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/webhook", nil)
		req.Header.Set("X-Request-Id", "019f1cb8-0000-7000-8000-000000000002")
		handler.ServeHTTP(httptest.NewRecorder(), req)

		lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
		require.Len(t, lines, 3)

		var handlerLine, sourceLine, completedLine map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[0]), &handlerLine))
		require.NoError(t, json.Unmarshal([]byte(lines[1]), &sourceLine))
		require.NoError(t, json.Unmarshal([]byte(lines[2]), &completedLine))

		require.Equal(t, "logged by the handler", handlerLine["@message"])
		require.Equal(t, "server:request:019f1cb8-0000-7000-8000-000000000002", handlerLine["@module"])
		require.Equal(t, "019f1cb8-0000-7000-8000-000000000002", handlerLine["requestId"])

		require.Equal(t, "logged by a source", sourceLine["@message"])
		require.Equal(t, "ibdm:source:fake", sourceLine["@module"])
		require.Equal(t, "019f1cb8-0000-7000-8000-000000000002", sourceLine["requestId"], "renaming the logger keeps the request id")

		require.Equal(t, RequestCompletedMessage, completedLine["@message"])
		require.Equal(t, "019f1cb8-0000-7000-8000-000000000002", completedLine["requestId"])
	})

	t.Run("excluded request carries the logger of the middleware", func(t *testing.T) {
		t.Parallel()

		logger := NewLogger(new(bytes.Buffer))
		var found Logger
		handler := RequestMiddlewareLogger(logger, []string{"/-/"})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			found = FromContext(r.Context())
		}))

		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/-/healthz", nil))

		require.Same(t, logger, found)
	})
}

func TestResponseRecorderUnwrap(t *testing.T) {
	t.Parallel()

	logger := NewLogger(new(bytes.Buffer))
	handler := RequestMiddlewareLogger(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("chunk"))
		require.NoError(t, http.NewResponseController(w).Flush())
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/stream", nil))

	require.True(t, recorder.Flushed)
	require.Equal(t, "chunk", recorder.Body.String())
}
