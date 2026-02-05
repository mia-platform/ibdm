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
}
func TestLoadValidateEnvironmentVariables(t *testing.T) {
	t.Parallel()
	t.Run("Environment variables validation", func(t *testing.T) {
		t.Parallel()
		envVars := &config{HTTPPort: -1}
		err := validateEnvironmentVariables(envVars)
		require.Error(t, err)
	})
	t.Run("Environment variables validation", func(t *testing.T) {
		t.Parallel()
		envVars := &config{HTTPPort: 655350}
		err := validateEnvironmentVariables(envVars)
		require.Error(t, err)
	})
	t.Run("Environment variables validation", func(t *testing.T) {
		t.Parallel()
		envVars := &config{HTTPPort: 3000}
		err := validateEnvironmentVariables(envVars)
		require.NoError(t, err)
	})
}
