// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package easm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigFromEnv(t *testing.T) {
	testCases := map[string]struct {
		setupEnv       func(t *testing.T)
		expectedConfig config
		expectedErr    error
	}{
		"required env vars set, defaults applied": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("EASM_BASE_URL", "https://easm.example.com")
				t.Setenv("EASM_CUSTOMER", "acme")
			},
			expectedConfig: config{
				BaseURL:     "https://easm.example.com",
				DataPath:    "/data",
				Customer:    "acme",
				HTTPTimeout: 30 * time.Second,
			},
		},
		"all env vars set including optional": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("EASM_BASE_URL", "https://easm.example.com")
				t.Setenv("EASM_CUSTOMER", "acme")
				t.Setenv("EASM_TOKEN", "secret")
				t.Setenv("EASM_HTTP_TIMEOUT", "10s")
			},
			expectedConfig: config{
				BaseURL:     "https://easm.example.com",
				DataPath:    "/data",
				Customer:    "acme",
				Token:       "secret",
				HTTPTimeout: 10 * time.Second,
			},
		},
		"missing EASM_BASE_URL": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("EASM_CUSTOMER", "acme")
			},
			expectedErr: ErrMissingEnvVariable,
		},
		"missing EASM_CUSTOMER": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("EASM_BASE_URL", "https://easm.example.com")
			},
			expectedErr: ErrMissingEnvVariable,
		},
		"missing all required vars": {
			setupEnv: func(t *testing.T) {
				t.Helper()
			},
			expectedErr: ErrMissingEnvVariable,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			tc.setupEnv(t)

			cfg, err := loadConfigFromEnv()
			if tc.expectedErr != nil {
				require.ErrorIs(t, err, tc.expectedErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expectedConfig, cfg)
		})
	}
}

func TestValidateConfig(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		config    config
		expectErr error
	}{
		"valid config": {
			config: config{
				BaseURL:     "https://easm.example.com",
				DataPath:    "/data",
				Customer:    "acme",
				HTTPTimeout: 30 * time.Second,
			},
		},
		"missing base URL": {
			config: config{
				Customer: "acme",
			},
			expectErr: ErrMissingEnvVariable,
		},
		"missing customer": {
			config: config{
				BaseURL: "https://easm.example.com",
			},
			expectErr: ErrMissingEnvVariable,
		},
		"all missing": {
			config:    config{},
			expectErr: ErrMissingEnvVariable,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := validateConfig(tc.config)
			if tc.expectErr != nil {
				assert.ErrorIs(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
