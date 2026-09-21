// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package easm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSource(t *testing.T) {
	testCases := map[string]struct {
		setupEnv    func(t *testing.T)
		expectErrs  []error
		assertValid func(t *testing.T, s *Source)
	}{
		"valid env": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("EASM_BASE_URL", "https://easm.example.com")
				t.Setenv("EASM_CUSTOMER", "acme")
			},
			assertValid: func(t *testing.T, s *Source) {
				t.Helper()
				assert.Equal(t, "https://easm.example.com", s.config.BaseURL)
				assert.Equal(t, "acme", s.config.Customer)
				assert.NotNil(t, s.client)
			},
		},
		"config error: missing required var": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("EASM_CUSTOMER", "acme")
			},
			expectErrs: []error{ErrEASMSource, ErrMissingEnvVariable},
		},
		"client error: invalid base URL": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("EASM_BASE_URL", "://invalid")
				t.Setenv("EASM_CUSTOMER", "acme")
			},
			expectErrs: []error{ErrEASMSource, ErrInvalidEnvVariable},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			tc.setupEnv(t)

			s, err := NewSource()
			if len(tc.expectErrs) > 0 {
				require.Error(t, err)
				for _, target := range tc.expectErrs {
					assert.ErrorIs(t, err, target)
				}
				assert.Nil(t, s)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, s)
			tc.assertValid(t, s)
		})
	}
}
