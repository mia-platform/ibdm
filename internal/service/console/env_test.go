// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigFromEnv(t *testing.T) {
	t.Run("fails when CONSOLE_ENDPOINT is missing", func(t *testing.T) {
		// Ideally we should ensure the env var is unset, but t.Setenv can only set values.
		// However, assuming the test runner environment doesn't have these set, or we can set it to empty.
		t.Setenv("CONSOLE_ENDPOINT", "")
		config, err := loadConfigFromEnv()
		require.Error(t, err)
		require.Nil(t, config)
	})

	t.Run("fails when CONSOLE_ENDPOINT is invalid URL", func(t *testing.T) {
		t.Setenv("CONSOLE_ENDPOINT", "://invalid-url")
		config, err := loadConfigFromEnv()
		require.Error(t, err)
		require.Nil(t, config)
		require.Contains(t, err.Error(), "invalid CONSOLE_ENDPOINT")
	})

	t.Run("fails when CONSOLE_CLIENT_ID is present but CONSOLE_CLIENT_SECRET is missing", func(t *testing.T) {
		t.Setenv("CONSOLE_ENDPOINT", "https://console.mia-platform.eu")
		t.Setenv("CONSOLE_CLIENT_ID", "client-id")
		t.Setenv("CONSOLE_CLIENT_SECRET", "")
		config, err := loadConfigFromEnv()
		require.Error(t, err)
		require.Nil(t, config)
		require.Contains(t, err.Error(), errMissingClientSecret.Error())
	})

	t.Run("fails when CONSOLE_CLIENT_SECRET is present but CONSOLE_CLIENT_ID is missing", func(t *testing.T) {
		t.Setenv("CONSOLE_ENDPOINT", "https://console.mia-platform.eu")
		t.Setenv("CONSOLE_CLIENT_ID", "")
		t.Setenv("CONSOLE_CLIENT_SECRET", "client-secret")
		config, err := loadConfigFromEnv()
		require.Error(t, err)
		require.Nil(t, config)
		require.Contains(t, err.Error(), errMissingClientID.Error())
	})

	t.Run("fails when CONSOLE_AUTH_ENDPOINT is invalid URL", func(t *testing.T) {
		t.Setenv("CONSOLE_ENDPOINT", "https://console.mia-platform.eu")
		t.Setenv("CONSOLE_AUTH_ENDPOINT", "://invalid-url")
		config, err := loadConfigFromEnv()
		require.Error(t, err)
		require.Nil(t, config)
		require.Contains(t, err.Error(), "invalid CONSOLE_AUTH_ENDPOINT")
	})

	t.Run("succeeds with minimal valid config", func(t *testing.T) {
		t.Setenv("CONSOLE_ENDPOINT", "https://console.mia-platform.eu")
		config, err := loadConfigFromEnv()
		require.NoError(t, err)
		require.NotNil(t, config)
		require.Equal(t, "https://console.mia-platform.eu", config.ConsoleEndpoint)
		// Check inferred AuthEndpoint
		require.Equal(t, "https://console.mia-platform.eu/oauth/token", config.AuthEndpoint)
	})

	t.Run("succeeds with full valid config", func(t *testing.T) {
		t.Setenv("CONSOLE_ENDPOINT", "https://console.mia-platform.eu")
		t.Setenv("CONSOLE_CLIENT_ID", "client-id")
		t.Setenv("CONSOLE_CLIENT_SECRET", "client-secret")
		t.Setenv("CONSOLE_AUTH_ENDPOINT", "https://auth.mia-platform.eu/oauth/token")
		config, err := loadConfigFromEnv()
		require.NoError(t, err)
		require.NotNil(t, config)
		require.Equal(t, "https://console.mia-platform.eu", config.ConsoleEndpoint)
		require.Equal(t, "client-id", config.ClientID)
		require.Equal(t, "client-secret", config.ClientSecret)
		require.Equal(t, "https://auth.mia-platform.eu/oauth/token", config.AuthEndpoint)
	})
}
