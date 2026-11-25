// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadEnvironmentVariables(t *testing.T) {
	t.Run("Load environment variables", func(t *testing.T) {
		t.Setenv("HTTP_PORT", "3000")
		envVars, err := LoadServerConfig()
		require.NoError(t, err)
		require.Equal(t, 3000, envVars.HTTPPort)
	})

	t.Run("Load environment variables", func(t *testing.T) {
		t.Setenv("HTTP_PORT", "655350")
		_, err := LoadServerConfig()
		require.Error(t, err)
	})

	t.Run("default body limit is 4 MiB", func(t *testing.T) {
		envVars, err := LoadServerConfig()
		require.NoError(t, err)
		require.Equal(t, int64(4*1024*1024), envVars.MaxBodySize)
	})

	t.Run("custom body limit", func(t *testing.T) {
		t.Setenv("HTTP_MAX_BODY_SIZE", "26214400")
		envVars, err := LoadServerConfig()
		require.NoError(t, err)
		require.Equal(t, int64(26214400), envVars.MaxBodySize)
	})

	for _, value := range []string{"0", "-1", "large"} {
		t.Run("invalid body limit "+value, func(t *testing.T) {
			t.Setenv("HTTP_MAX_BODY_SIZE", value)
			_, err := LoadServerConfig()
			require.ErrorIs(t, err, ErrEnvVariablesNotValid)
		})
	}
}
func TestLoadValidateEnvironmentVariables(t *testing.T) {
	t.Parallel()
	t.Run("Environment variables validation", func(t *testing.T) {
		t.Parallel()
		envVars := &config{HTTPPort: -1, MaxBodySize: 1}
		err := validateEnvironmentVariables(envVars)
		require.Error(t, err)
	})
	t.Run("Environment variables validation", func(t *testing.T) {
		t.Parallel()
		envVars := &config{HTTPPort: 655350, MaxBodySize: 1}
		err := validateEnvironmentVariables(envVars)
		require.Error(t, err)
	})
	t.Run("Environment variables validation", func(t *testing.T) {
		t.Parallel()
		envVars := &config{HTTPPort: 3000, MaxBodySize: 1}
		err := validateEnvironmentVariables(envVars)
		require.NoError(t, err)
	})
}
