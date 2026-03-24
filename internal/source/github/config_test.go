// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigFromEnv(t *testing.T) {
	testCases := map[string]struct {
		envVars     map[string]string
		expectErr   error
		expectCfg   *config
		expectNoErr bool
	}{
		"valid full configuration": {
			envVars: map[string]string{
				"GITHUB_TOKEN":          "ghp_test123",
				"GITHUB_ORG":            "mia-platform",
				"GITHUB_URL":            "https://github.example.com/api/v3",
				"GITHUB_HTTP_TIMEOUT":   "10s",
				"GITHUB_PAGE_SIZE":      "50",
				"GITHUB_WEBHOOK_SECRET": "mysecret",
				"GITHUB_WEBHOOK_PATH":   "/webhook/custom",
			},
			expectNoErr: true,
			expectCfg: &config{
				URL:           "https://github.example.com/api/v3",
				Token:         "ghp_test123",
				Org:           "mia-platform",
				HTTPTimeout:   10_000_000_000,
				PageSize:      50,
				WebhookSecret: "mysecret",
				WebhookPath:   "/webhook/custom",
			},
		},
		"valid minimal configuration uses defaults": {
			envVars: map[string]string{
				"GITHUB_TOKEN": "ghp_test123",
				"GITHUB_ORG":   "mia-platform",
			},
			expectNoErr: true,
			expectCfg: &config{
				URL:         "https://api.github.com",
				Token:       "ghp_test123",
				Org:         "mia-platform",
				HTTPTimeout: 30_000_000_000,
				PageSize:    100,
				WebhookPath: "/webhook/github",
			},
		},
		"missing GITHUB_TOKEN returns error": {
			envVars: map[string]string{
				"GITHUB_ORG": "mia-platform",
			},
			expectErr: ErrMissingEnvVariable,
		},
		"missing GITHUB_ORG returns error": {
			envVars: map[string]string{
				"GITHUB_TOKEN": "ghp_test123",
			},
			expectErr: ErrMissingEnvVariable,
		},
		"page size at lower bound is accepted": {
			envVars: map[string]string{
				"GITHUB_TOKEN":     "ghp_test123",
				"GITHUB_ORG":       "mia-platform",
				"GITHUB_PAGE_SIZE": "1",
			},
			expectNoErr: true,
			expectCfg: &config{
				URL:         "https://api.github.com",
				Token:       "ghp_test123",
				Org:         "mia-platform",
				HTTPTimeout: 30_000_000_000,
				PageSize:    1,
				WebhookPath: "/webhook/github",
			},
		},
		"page size at upper bound is accepted": {
			envVars: map[string]string{
				"GITHUB_TOKEN":     "ghp_test123",
				"GITHUB_ORG":       "mia-platform",
				"GITHUB_PAGE_SIZE": "100",
			},
			expectNoErr: true,
			expectCfg: &config{
				URL:         "https://api.github.com",
				Token:       "ghp_test123",
				Org:         "mia-platform",
				HTTPTimeout: 30_000_000_000,
				PageSize:    100,
				WebhookPath: "/webhook/github",
			},
		},
		"page size below lower bound returns error": {
			envVars: map[string]string{
				"GITHUB_TOKEN":     "ghp_test123",
				"GITHUB_ORG":       "mia-platform",
				"GITHUB_PAGE_SIZE": "0",
			},
			expectErr: ErrInvalidEnvVariable,
		},
		"page size above upper bound returns error": {
			envVars: map[string]string{
				"GITHUB_TOKEN":     "ghp_test123",
				"GITHUB_ORG":       "mia-platform",
				"GITHUB_PAGE_SIZE": "101",
			},
			expectErr: ErrInvalidEnvVariable,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// t.Setenv is incompatible with t.Parallel()
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			cfg, err := loadConfigFromEnv()
			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
				assert.Nil(t, cfg)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)
			assert.Equal(t, tc.expectCfg, cfg)
		})
	}
}
