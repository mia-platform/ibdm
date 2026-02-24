// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSourceConfigFromEnv(t *testing.T) {
	testCases := map[string]struct {
		setEnv      func(t *testing.T)
		expectedCfg sourceConfig
		expectErr   bool
	}{
		"all env set": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_TOKEN", "my-token")
				t.Setenv("GITLAB_BASE_URL", "https://gitlab.example.com")
			},
			expectedCfg: sourceConfig{Token: "my-token", BaseURL: "https://gitlab.example.com"},
		},
		"default base URL": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_TOKEN", "my-token")
			},
			expectedCfg: sourceConfig{Token: "my-token", BaseURL: "https://gitlab.com"},
		},
		"missing required token": {
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			if tc.setEnv != nil {
				tc.setEnv(t)
			}

			cfg, err := loadSourceConfigFromEnv()
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedCfg, cfg)
		})
	}
}

func TestLoadWebhookConfigFromEnv(t *testing.T) {
	testCases := map[string]struct {
		setEnv      func(t *testing.T)
		expectedCfg webhookConfig
		expectErr   bool
	}{
		"all env set": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_WEBHOOK_PATH", "/hooks/gitlab")
				t.Setenv("GITLAB_WEBHOOK_TOKEN", "secret")
			},
			expectedCfg: webhookConfig{WebhookPath: "/hooks/gitlab", WebhookToken: "secret"},
		},
		"default path": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_WEBHOOK_TOKEN", "secret")
			},
			expectedCfg: webhookConfig{WebhookPath: "/gitlab/webhook", WebhookToken: "secret"},
		},
		"no token is valid": {
			expectedCfg: webhookConfig{WebhookPath: "/gitlab/webhook"},
		},
		"path without leading slash": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GITLAB_WEBHOOK_PATH", "hooks/gitlab")
			},
			expectErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			if tc.setEnv != nil {
				tc.setEnv(t)
			}

			cfg, err := loadWebhookConfigFromEnv()
			if tc.expectErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrWebhookConfigNotValid)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedCfg, cfg)
		})
	}
}
