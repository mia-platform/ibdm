// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigFromEnv(t *testing.T) {
	t.Run("valid configuration", func(t *testing.T) {
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")
		t.Setenv("CONSOLE_WEBHOOK_SECRET", "secret")
		config, err := loadConfigFromEnv()
		require.NoError(t, err)
		require.Equal(t, "/webhook", config.WebhookPath)
		require.Equal(t, "secret", config.WebhookSecret)
	})

	t.Run("default configuration - missing path", func(t *testing.T) {
		t.Setenv("CONSOLE_WEBHOOK_SECRET", "secret")
		config, err := loadConfigFromEnv()
		require.NoError(t, err)
		require.Equal(t, "/console/webhook", config.WebhookPath)
		require.Equal(t, "secret", config.WebhookSecret)
	})

	t.Run("default configuration - empty string value", func(t *testing.T) {
		t.Setenv("CONSOLE_WEBHOOK_PATH", "")
		t.Setenv("CONSOLE_WEBHOOK_SECRET", "secret")
		config, err := loadConfigFromEnv()
		require.NoError(t, err)
		require.Equal(t, "/console/webhook", config.WebhookPath)
		require.Equal(t, "secret", config.WebhookSecret)
	})

	t.Run("invalid configuration - wrong path", func(t *testing.T) {
		t.Setenv("CONSOLE_WEBHOOK_PATH", "webhook")
		t.Setenv("CONSOLE_WEBHOOK_SECRET", "secret")
		_, err := loadConfigFromEnv()
		require.ErrorIs(t, err, ErrWebhookConfigNotValid)
	})
}
