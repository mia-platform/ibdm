// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

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
		"all required env vars set": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("NEXUS_URL", "https://nexus.example.com")
				t.Setenv("NEXUS_TOKEN_NAME", "mytoken")
				t.Setenv("NEXUS_TOKEN_PASSCODE", "secret")
			},
			expectedConfig: config{
				URL:           "https://nexus.example.com",
				TokenName:     "mytoken",
				TokenPasscode: "secret",
				HTTPTimeout:   30 * time.Second,
			},
		},
		"all env vars set including optional": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("NEXUS_URL", "https://nexus.example.com")
				t.Setenv("NEXUS_TOKEN_NAME", "mytoken")
				t.Setenv("NEXUS_TOKEN_PASSCODE", "secret")
				t.Setenv("NEXUS_HTTP_TIMEOUT", "10s")
				t.Setenv("NEXUS_SPECIFIC_REPOSITORY", "maven-central")
			},
			expectedConfig: config{
				URL:                "https://nexus.example.com",
				TokenName:          "mytoken",
				TokenPasscode:      "secret",
				HTTPTimeout:        10 * time.Second,
				SpecificRepository: "maven-central",
			},
		},
		"missing NEXUS_URL": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("NEXUS_TOKEN_NAME", "mytoken")
				t.Setenv("NEXUS_TOKEN_PASSCODE", "secret")
			},
			expectedErr: ErrMissingEnvVariable,
		},
		"missing NEXUS_TOKEN_NAME": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("NEXUS_URL", "https://nexus.example.com")
				t.Setenv("NEXUS_TOKEN_PASSCODE", "secret")
			},
			expectedErr: ErrMissingEnvVariable,
		},
		"missing NEXUS_TOKEN_PASSCODE": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("NEXUS_URL", "https://nexus.example.com")
				t.Setenv("NEXUS_TOKEN_NAME", "mytoken")
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
				URL:           "https://nexus.example.com",
				TokenName:     "mytoken",
				TokenPasscode: "secret",
				HTTPTimeout:   30 * time.Second,
			},
		},
		"missing URL": {
			config: config{
				TokenName:     "mytoken",
				TokenPasscode: "secret",
			},
			expectErr: ErrMissingEnvVariable,
		},
		"missing token name": {
			config: config{
				URL:           "https://nexus.example.com",
				TokenPasscode: "secret",
			},
			expectErr: ErrMissingEnvVariable,
		},
		"missing token passcode": {
			config: config{
				URL:       "https://nexus.example.com",
				TokenName: "mytoken",
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
