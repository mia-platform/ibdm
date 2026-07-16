// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeTestFile writes content to path, failing the test immediately on error. It is used to
// materialize fictional key material on disk for tests that exercise file-based configuration.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
