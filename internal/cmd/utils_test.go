// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source/fake"
)

// setupTestFileStructure creates a test file structure under the given baseDir.
func setupTestFileStructure(tb testing.TB, baseDir string) {
	tb.Helper()

	if err := os.MkdirAll(filepath.Join(baseDir, "valid", "subdir"), os.ModePerm); err != nil {
		require.NoError(tb, err)
	}

	if err := os.Symlink(filepath.Join(baseDir, "valid", "subdir"), filepath.Join(baseDir, "valid", "link")); err != nil {
		require.NoError(tb, err)
	}

	if err := os.WriteFile(filepath.Join(baseDir, "valid", "invalid.yaml"), []byte("\tinvalid yaml file"), os.ModePerm); err != nil {
		require.NoError(tb, err)
	}

	if err := os.WriteFile(filepath.Join(baseDir, "valid", "subdir", "file.txt"), []byte("txt file"), os.ModePerm); err != nil {
		require.NoError(tb, err)
	}

	if err := os.Symlink(filepath.Join(baseDir, "valid", "invalid.yaml"), filepath.Join(baseDir, "symlink.file")); err != nil {
		require.NoError(tb, err)
	}

	if err := os.Mkdir(filepath.Join(baseDir, "secret"), os.ModePerm); err != nil {
		require.NoError(tb, err)
	}
	if err := os.Chmod(filepath.Join(baseDir, "secret"), 0o0000); err != nil {
		require.NoError(tb, err)
	}
}

// return a fake source getter for testing purposes.
func testSourceGetter(tb testing.TB) func(string) (any, error) {
	tb.Helper()

	return func(integrationName string) (any, error) {
		switch integrationName {
		case "fake":
			return fake.NewFakeSourceWithError(tb, nil), nil
		case "unsupported":
			return "unsupported source type", nil
		}

		return nil, assert.AnError
	}
}
