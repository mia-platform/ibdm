// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sysdig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigFromEnv(t *testing.T) {
	t.Run("valid full configuration", func(t *testing.T) {
		t.Setenv("SYSDIG_URL", "https://secure.sysdig.com")
		t.Setenv("SYSDIG_API_TOKEN", "test-token")
		t.Setenv("SYSDIG_HTTP_TIMEOUT", "15s")
		t.Setenv("SYSDIG_PAGE_SIZE", "500")

		cfg, err := loadConfigFromEnv()
		require.NoError(t, err)
		assert.Equal(t, "https://secure.sysdig.com", cfg.URL)
		assert.Equal(t, "test-token", cfg.APIToken)
		assert.Equal(t, 15_000_000_000, int(cfg.HTTPTimeout))
		assert.Equal(t, 500, cfg.PageSize)
	})

	t.Run("valid minimal configuration with defaults", func(t *testing.T) {
		t.Setenv("SYSDIG_URL", "https://secure.sysdig.com")
		t.Setenv("SYSDIG_API_TOKEN", "test-token")

		cfg, err := loadConfigFromEnv()
		require.NoError(t, err)
		assert.Equal(t, 30_000_000_000, int(cfg.HTTPTimeout))
		assert.Equal(t, 1000, cfg.PageSize)
	})
}

func TestConfigValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config    config
		expectErr error
	}{
		"valid config": {
			config: config{
				URL:      "https://secure.sysdig.com",
				APIToken: "token",
				PageSize: 500,
			},
		},
		"missing URL": {
			config: config{
				APIToken: "token",
				PageSize: 500,
			},
			expectErr: ErrMissingEnvVariable,
		},
		"missing API token": {
			config: config{
				URL:      "https://secure.sysdig.com",
				PageSize: 500,
			},
			expectErr: ErrMissingEnvVariable,
		},
		"page size too small": {
			config: config{
				URL:      "https://secure.sysdig.com",
				APIToken: "token",
				PageSize: 0,
			},
			expectErr: ErrInvalidEnvVariable,
		},
		"page size too large": {
			config: config{
				URL:      "https://secure.sysdig.com",
				APIToken: "token",
				PageSize: sysdigMaxPageSize + 1,
			},
			expectErr: ErrInvalidEnvVariable,
		},
		"page size at lower bound": {
			config: config{
				URL:      "https://secure.sysdig.com",
				APIToken: "token",
				PageSize: 1,
			},
		},
		"page size at upper bound": {
			config: config{
				URL:      "https://secure.sysdig.com",
				APIToken: "token",
				PageSize: sysdigMaxPageSize,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := tc.config.validate()
			if tc.expectErr != nil {
				assert.ErrorIs(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
