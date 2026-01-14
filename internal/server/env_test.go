// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadEnvironmentVariables(t *testing.T) {
	t.Run("Environment variables validation", func(t *testing.T) {
		envVars := &EnvironmentVariables{HTTPPort: ""}
		err := validateEnvironmentVariables(envVars)
		require.Error(t, err)
	})

	t.Run("Load environment variables", func(t *testing.T) {
		t.Setenv("HTTP_PORT", "3000")
		envVars, err := LoadServerEnvs()
		require.NoError(t, err)
		require.Equal(t, "3000", envVars.HTTPPort)
	})
}
