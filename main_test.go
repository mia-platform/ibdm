// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package main

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCommand(t *testing.T) {
	t.Parallel()

	Version = "test"
	BuildDate = "2024-06-01"

	cmd := rootCmd()
	buffer := new(bytes.Buffer)
	cmd.SetOut(buffer)

	log := logger.NewLogger(cmd.OutOrStderr())
	ctx := logger.WithContext(t.Context(), log)

	cmd.SetArgs([]string{"--log-level", "WARN", "version"})
	err := cmd.ExecuteContext(ctx)
	require.NoError(t, err)

	log.Info("ignored line for set log level")
	lines := strings.Split(buffer.String(), "\n")
	assert.Len(t, lines, 2) // version output + empty line
	assert.Equal(t, versionString(Version, BuildDate, runtime.Version())+"\n", buffer.String())

	buffer.Reset()
	BuildDate = ""
	cmd.SetArgs([]string{"--log-level", "WARN", "version"})
	err = cmd.ExecuteContext(ctx)
	require.NoError(t, err)
	assert.Len(t, lines, 2) // version output + empty line
	assert.Equal(t, versionString(Version, "", runtime.Version())+"\n", buffer.String())
}
