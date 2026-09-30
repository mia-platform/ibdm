// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigFromEnv(t *testing.T) {
	t.Run("valid full configuration", func(t *testing.T) {
		t.Setenv("SONARQUBE_URL", "https://sonarqube.example.com/")
		t.Setenv("SONARQUBE_PUBLIC_URL", "https://sonar.example.com/")
		t.Setenv("SONARQUBE_TOKEN", "test-token")
		t.Setenv("SONARQUBE_HTTP_TIMEOUT", "15s")
		t.Setenv("SONARQUBE_ISSUE_STATUSES", "OPEN,CONFIRMED,ACCEPTED")
		t.Setenv("SONARQUBE_NEW_CODE_ONLY", "true")
		t.Setenv("SONARQUBE_MAX_ISSUES", "50")
		t.Setenv("SONARQUBE_PROJECT_KEYS", "first, second,,")
		t.Setenv("SONARQUBE_SCM_PROVIDER", "gitlab")

		cfg, err := loadConfigFromEnv()
		require.NoError(t, err)
		assert.Equal(t, "https://sonarqube.example.com", cfg.URL)
		assert.Equal(t, "https://sonar.example.com", cfg.PublicURL)
		assert.Equal(t, "test-token", cfg.Token)
		assert.Equal(t, 15*time.Second, cfg.HTTPTimeout)
		assert.Equal(t, "OPEN,CONFIRMED,ACCEPTED", cfg.IssueStatuses)
		assert.True(t, cfg.NewCodeOnly)
		assert.Equal(t, 50, cfg.MaxIssues)
		assert.Equal(t, []string{"first", "second"}, cfg.ProjectKeys)
		assert.Equal(t, "gitlab", cfg.SCMProvider)
	})

	t.Run("valid minimal configuration with defaults", func(t *testing.T) {
		t.Setenv("SONARQUBE_URL", "https://sonarqube.example.com")
		t.Setenv("SONARQUBE_TOKEN", "test-token")

		cfg, err := loadConfigFromEnv()
		require.NoError(t, err)
		assert.Equal(t, 30*time.Second, cfg.HTTPTimeout)
		assert.Equal(t, "OPEN,CONFIRMED", cfg.IssueStatuses)
		assert.False(t, cfg.NewCodeOnly)
		assert.Equal(t, 20000, cfg.MaxIssues)
		assert.Empty(t, cfg.ProjectKeys)
		assert.Equal(t, "auto", cfg.SCMProvider)
	})

	t.Run("missing required variables are named", func(t *testing.T) {
		_, err := loadConfigFromEnv()
		require.ErrorIs(t, err, ErrMissingEnvVariable)
		assert.Contains(t, err.Error(), "SONARQUBE_URL")
		assert.Contains(t, err.Error(), "SONARQUBE_TOKEN")
	})
}

func TestConfigValidation(t *testing.T) {
	t.Parallel()

	valid := config{URL: "https://sonarqube.example.com", Token: "token", MaxIssues: 1}

	tests := map[string]struct {
		mutate    func(*config)
		expectErr error
	}{
		"valid config":                    {mutate: func(*config) {}},
		"url without scheme":              {mutate: func(c *config) { c.URL = "sonarqube.example.com" }, expectErr: ErrInvalidEnvVariable},
		"url with a non-http scheme":      {mutate: func(c *config) { c.URL = "ftp://sonarqube.example.com" }, expectErr: ErrInvalidEnvVariable},
		"invalid public url":              {mutate: func(c *config) { c.PublicURL = "javascript:alert(1)" }, expectErr: ErrInvalidEnvVariable},
		"max issues too small":            {mutate: func(c *config) { c.MaxIssues = 0 }, expectErr: ErrInvalidEnvVariable},
		"unknown scm provider":            {mutate: func(c *config) { c.SCMProvider = "unknown-forge" }, expectErr: ErrInvalidEnvVariable},
		"forgiving scm provider spelling": {mutate: func(c *config) { c.SCMProvider = "Bitbucket_Server" }},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := valid
			tc.mutate(&cfg)
			err := cfg.validate()
			if tc.expectErr != nil {
				assert.ErrorIs(t, err, tc.expectErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestLoadWebhookConfigFromEnv(t *testing.T) {
	t.Run("full configuration", func(t *testing.T) {
		t.Setenv("SONARQUBE_WEBHOOK_PATH", "/custom/webhook")
		t.Setenv("SONARQUBE_WEBHOOK_SECRET", "secret")
		t.Setenv("SONARQUBE_WEBHOOK_ALLOW_UNSIGNED", "true")

		cfg, err := loadWebhookConfigFromEnv()
		require.NoError(t, err)
		assert.Equal(t, "/custom/webhook", cfg.WebhookPath)
		assert.Equal(t, "secret", cfg.WebhookSecret)
		assert.True(t, cfg.AllowUnsigned)
	})

	t.Run("defaults only", func(t *testing.T) {
		cfg, err := loadWebhookConfigFromEnv()
		require.NoError(t, err)
		assert.Equal(t, "/sonarqube/webhook", cfg.WebhookPath)
		assert.Empty(t, cfg.WebhookSecret)
		assert.False(t, cfg.AllowUnsigned)
	})
}
