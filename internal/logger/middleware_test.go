// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package logger

import (
	"bytes"
	"encoding/json"
	nhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestRequestMiddlewareLogger(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		excludedPrefixes []string
		expectedLogLines int
		loggerLevel      Level
		path             string
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
			method:           nhttp.MethodGet,
			userAgent:        "test-agent",
			host:             "example.com",
			statusCode:       nhttp.StatusOK,
		},
		"skips excluded paths": {
			excludedPrefixes: []string{"/health"},
			expectedLogLines: 0,
			loggerLevel:      TRACE,
			path:             "/health",
			method:           nhttp.MethodGet,
			userAgent:        "test-agent",
			host:             "example.com",
			statusCode:       nhttp.StatusOK,
		},
		"skips incoming request log due to logger level": {
			excludedPrefixes: []string{},
			expectedLogLines: 1,
			loggerLevel:      INFO,
			path:             "/foo",
			method:           nhttp.MethodGet,
			userAgent:        "test-agent",
			host:             "example.com",
			statusCode:       nhttp.StatusOK,
		},
		"skips all logs due to excluded path and logger level": {
			excludedPrefixes: []string{"/foo"},
			expectedLogLines: 0,
			loggerLevel:      INFO,
			path:             "/foo",
			method:           nhttp.MethodGet,
			userAgent:        "test-agent",
			host:             "example.com",
			statusCode:       nhttp.StatusOK,
		},
		"method POST logs incoming and completed requests": {
			excludedPrefixes: []string{},
			expectedLogLines: 2,
			loggerLevel:      TRACE,
			path:             "/bar",
			method:           nhttp.MethodPost,
			userAgent:        "test-agent",
			host:             "example.com",
			statusCode:       nhttp.StatusOK,
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			buffer := new(bytes.Buffer)
			logger := NewLogger(buffer)
			logger.SetLevel(test.loggerLevel)

			app := fiber.New()
			app.Use(RequestMiddlewareLogger(logger, test.excludedPrefixes))

			app.Get(test.path, func(c *fiber.Ctx) error {
				return c.SendString("foo")
			})

			req := httptest.NewRequest(test.method, test.path, nil)
			req.Header.Set("User-Agent", test.userAgent)
			req.Host = test.host
			resp, err := app.Test(req)
			require.NoError(t, err)
			defer resp.Body.Close()

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

			var firstLog map[string]any
			err = json.Unmarshal([]byte(lines[0]), &firstLog)
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
				require.Equal(t, test.statusCode, firstLog["http"].(map[string]any)["response"].(map[string]any)["statusCode"])
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
