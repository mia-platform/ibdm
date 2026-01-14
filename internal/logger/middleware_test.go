// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package logger

import (
	"bytes"
	netHTTP "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestRequestMiddlewareLogger(t *testing.T) {
	t.Parallel()

	buffer := new(bytes.Buffer)
	logger := NewLogger(buffer)
	logger.SetLevel(TRACE)

	app := fiber.New(fiber.Config{})
	require.NotNil(t, app)

	middleware := RequestMiddlewareLogger(logger, []string{"/-/healthz"})
	require.NotNil(t, middleware)

	app.Use(middleware)

	req := httptest.NewRequest(netHTTP.MethodGet, "http://example.com/foo", nil)
	req.Header.Set("User-Agent", "UnitTestAgent/1.0")
	req.RemoteAddr = "127.0.0.1:12345"

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	logs := buffer.String()
	splitted := strings.Split(logs, "\n")
	require.Len(t, splitted, 3)
	require.Empty(t, splitted[2])
	// assert.Fail(t, "Check logs", "Logs captured:\n%s", logs)
}
