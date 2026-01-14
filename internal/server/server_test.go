// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewApp(t *testing.T) {
	t.Run("successfully creates app with valid config", func(t *testing.T) {
		ctx := t.Context()
		envs := EnvironmentVariables{
			LoggerLevel:           "info",
			DisableStartupMessage: true,
		}

		app, cleanup := NewServer(ctx, envs)
		require.NotNil(t, app)
		require.NotNil(t, cleanup)

		time.Sleep(1 * time.Second)
		request := httptest.NewRequest(http.MethodGet, "/-/healthz", nil)
		response, err := app.Test(request)
		require.NoError(t, err)

		defer response.Body.Close()
		require.Equal(t, http.StatusOK, response.StatusCode)
		defer cleanup()
	})
}
