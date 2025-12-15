// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source/gcp"
)

func TestCompletion(t *testing.T) {
	t.Parallel()
	testCases := map[string]struct {
		args               []string
		toComplete         string
		expectedCompletion []string
	}{
		"no args, complete root commands": {
			args: []string{},
			expectedCompletion: []string{
				"gcp\tGoogle Cloud Platform integration",
			},
		},
		"some args, no completions": {
			args: []string{"gcp"},
		},
		"no args, partial string, return filtered commands": {
			args:       []string{},
			toComplete: "g",
			expectedCompletion: []string{
				"gcp\tGoogle Cloud Platform integration",
			},
		},
		"no args, partial wrong string, return no command": {
			args:       []string{},
			toComplete: "x",
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			cmd := RunCmd()
			args, directive := validArgsFunc(availableEventSources)(cmd, test.args, test.toComplete)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
			assert.Equal(t, test.expectedCompletion, args)
		})
	}
}

func setupTestFileStructure(t *testing.T, baseDir string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(baseDir, "valid", "subdir"), os.ModePerm); err != nil {
		require.NoError(t, err)
	}

	if err := os.WriteFile(filepath.Join(baseDir, "valid", "invalid.yaml"), []byte("\tinvalid yaml file"), os.ModePerm); err != nil {
		require.NoError(t, err)
	}

	if err := os.Mkdir(filepath.Join(baseDir, "secret"), os.ModePerm); err != nil {
		require.NoError(t, err)
	}
	if err := os.Chmod(filepath.Join(baseDir, "secret"), 0o0000); err != nil {
		require.NoError(t, err)
	}
}

func TestSourceFromName(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		integrationName    string
		expectedSourceType any
	}{
		"gcp integration": {
			integrationName:    "gcp",
			expectedSourceType: (*gcp.GCPSource)(nil),
		},
		"invalid integration": {
			integrationName: "invalid",
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			source, err := sourceFromIntegrationName(t.Context(), test.integrationName)
			assert.NoError(t, err)
			if test.expectedSourceType == nil {
				assert.Nil(t, source)
				return
			}

			require.NotNil(t, source)
			assert.IsType(t, test.expectedSourceType, source)
		})
	}
}
