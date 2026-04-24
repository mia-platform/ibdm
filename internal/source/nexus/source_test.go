// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSource(t *testing.T) {
	testCases := map[string]struct {
		setupEnv    func(t *testing.T)
		expectErr   bool
		expectErrIs error
	}{
		"valid configuration": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("NEXUS_URL_SCHEMA", "https")
				t.Setenv("NEXUS_URL_HOST", "nexus.example.com")
				t.Setenv("NEXUS_TOKEN_NAME", "mytoken")
				t.Setenv("NEXUS_TOKEN_PASSCODE", "secret")
			},
		},
		"missing required env vars": {
			setupEnv: func(t *testing.T) {
				t.Helper()
			},
			expectErr:   true,
			expectErrIs: ErrNexusSource,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			tc.setupEnv(t)
			s, err := NewSource()
			if tc.expectErr {
				require.Error(t, err)
				if tc.expectErrIs != nil {
					require.ErrorIs(t, err, tc.expectErrIs)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, s)
		})
	}
}
