// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSourceConfigFromEnvParseError(t *testing.T) {
	// BITBUCKET_HTTP_TIMEOUT with an invalid duration string triggers ParseAs error.
	t.Setenv("BITBUCKET_ACCESS_TOKEN", "bbtoken")
	t.Setenv("BITBUCKET_HTTP_TIMEOUT", "invalid-duration")

	_, err := loadSourceConfigFromEnv()
	require.Error(t, err)
}

func TestLoadSourceConfigFromEnv(t *testing.T) {
	testCases := map[string]struct {
		envVars   map[string]string
		expectErr error
		expectCfg sourceConfig
	}{
		"valid configuration with bearer token": {
			envVars: map[string]string{
				"BITBUCKET_ACCESS_TOKEN": "bbtoken123",
			},
			expectCfg: sourceConfig{
				URL:         "https://api.bitbucket.org",
				AccessToken: "bbtoken123",
				HTTPTimeout: 30 * time.Second,
			},
		},
		"valid configuration with basic auth": {
			envVars: map[string]string{
				"BITBUCKET_API_USERNAME": "myuser",
				"BITBUCKET_API_TOKEN":    "mytoken",
			},
			expectCfg: sourceConfig{
				URL:         "https://api.bitbucket.org",
				APIUsername: "myuser",
				APIToken:    "mytoken",
				HTTPTimeout: 30 * time.Second,
			},
		},
		"valid full configuration with bearer token": {
			envVars: map[string]string{
				"BITBUCKET_ACCESS_TOKEN": "bbtoken123",
				"BITBUCKET_URL":          "https://custom.bitbucket.example.com",
				"BITBUCKET_HTTP_TIMEOUT": "10s",
				"BITBUCKET_WORKSPACE":    "my-workspace",
			},
			expectCfg: sourceConfig{
				URL:         "https://custom.bitbucket.example.com",
				AccessToken: "bbtoken123",
				HTTPTimeout: 10 * time.Second,
				Workspace:   "my-workspace",
			},
		},
		"neither auth mode set returns error": {
			envVars:   map[string]string{},
			expectErr: ErrMissingEnvVariable,
		},
		"both auth modes set returns error": {
			envVars: map[string]string{
				"BITBUCKET_ACCESS_TOKEN": "bbtoken123",
				"BITBUCKET_API_USERNAME": "myuser",
				"BITBUCKET_API_TOKEN":    "mytoken",
			},
			expectErr: ErrInvalidEnvVariable,
		},
		"only username without token returns error": {
			envVars: map[string]string{
				"BITBUCKET_API_USERNAME": "myuser",
			},
			expectErr: ErrMissingEnvVariable,
		},
		"only token without username returns error": {
			envVars: map[string]string{
				"BITBUCKET_API_TOKEN": "mytoken",
			},
			expectErr: ErrMissingEnvVariable,
		},
		"default values are applied correctly": {
			envVars: map[string]string{
				"BITBUCKET_ACCESS_TOKEN": "bbtoken123",
			},
			expectCfg: sourceConfig{
				URL:         "https://api.bitbucket.org",
				AccessToken: "bbtoken123",
				HTTPTimeout: 30 * time.Second,
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			cfg, err := loadSourceConfigFromEnv()
			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expectCfg, cfg)
		})
	}
}

func TestLoadWebhookConfigFromEnv(t *testing.T) {
	testCases := map[string]struct {
		envVars   map[string]string
		expectCfg webhookConfig
	}{
		"default values": {
			envVars: map[string]string{},
			expectCfg: webhookConfig{
				WebhookPath: "/bitbucket/webhook",
			},
		},
		"custom values": {
			envVars: map[string]string{
				"BITBUCKET_WEBHOOK_SECRET": "mysecret",
				"BITBUCKET_WEBHOOK_PATH":   "/custom/webhook",
			},
			expectCfg: webhookConfig{
				WebhookSecret: "mysecret",
				WebhookPath:   "/custom/webhook",
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			cfg, err := loadWebhookConfigFromEnv()
			require.NoError(t, err)
			assert.Equal(t, tc.expectCfg, cfg)
		})
	}
}
