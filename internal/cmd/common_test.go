// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/pipeline"
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

			args, directive := validArgsFunc(availableEventSources)(nil, test.args, test.toComplete)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
			assert.ElementsMatch(t, test.expectedCompletion, args)
		})
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
			expectedSourceType: (*gcp.Source)(nil),
		},
		"invalid integration": {
			integrationName: "invalid",
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			source, err := sourceFromIntegrationName(test.integrationName)
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

func TestCollectPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupTestFileStructure(t, tmpDir)
	testCases := map[string]struct {
		paths         []string
		expectedFiles []string
		expectedError error
	}{
		"single file": {
			paths: []string{
				filepath.Join(tmpDir, "valid", "subdir", "file.txt"),
			},
			expectedFiles: []string{
				filepath.Join(tmpDir, "valid", "subdir", "file.txt"),
			},
		},
		"directory with files and subdirectories": {
			paths: []string{
				filepath.Join(tmpDir, "valid"),
			},
			expectedFiles: []string{
				filepath.Join(tmpDir, "valid", "invalid.yaml"),
			},
		},
		"file and directory": {
			paths: []string{
				filepath.Join(tmpDir, "valid", "subdir", "file.txt"),
				filepath.Join(tmpDir, "valid"),
			},
			expectedFiles: []string{
				filepath.Join(tmpDir, "valid", "subdir", "file.txt"),
				filepath.Join(tmpDir, "valid", "invalid.yaml"),
			},
		},
		"non existent path": {
			paths: []string{
				filepath.Join(tmpDir, "nonexistent"),
			},
			expectedError: os.ErrNotExist,
		},
		"permission denied path": {
			paths: []string{
				filepath.Join(tmpDir, "secret"),
			},
			expectedError: os.ErrPermission,
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			files, err := collectPaths(test.paths)
			if test.expectedError != nil {
				assert.ErrorIs(t, err, test.expectedError)
				assert.Empty(t, files)
				return
			}

			assert.NoError(t, err)
			assert.ElementsMatch(t, test.expectedFiles, files)
		})
	}
}

func TestLoadMappers(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		paths           []string
		syncOnly        bool
		expectedMappers map[string]pipeline.DataMapper
		expectedError   error
	}{
		"valid mapping config": {
			paths: []string{
				filepath.Join("testdata", "mappers.yaml"),
			},
			expectedMappers: map[string]pipeline.DataMapper{
				"valid": {
					APIVersion: "v1",
					Resource:   "resource",
				},
				"mapper-type": {
					APIVersion: "v1",
					Resource:   "resource",
				},
			},
		},
		"valid mapping config filtered by sync": {
			paths: []string{
				filepath.Join("testdata", "mappers.yaml"),
			},
			syncOnly: true,
			expectedMappers: map[string]pipeline.DataMapper{
				"mapper-type": {
					APIVersion: "v1",
					Resource:   "resource",
				},
			},
		},
		"error reading config": {
			paths: []string{
				filepath.Join("testdata", "invalid-config.txt"),
			},
			expectedError: config.ErrParsing,
		},
		"error in mapping definition": {
			paths: []string{
				filepath.Join("testdata", "invalid.yaml"),
			},
			expectedError: mapper.NewParsingError(errors.Join(errors.New("template: spec:2: function \"invalidFunc\" not defined"))),
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			mappers, err := loadMappers(test.paths, test.syncOnly)
			if test.expectedError != nil {
				assert.ErrorIs(t, err, test.expectedError)
				return
			}

			assert.NoError(t, err)
			// custom equality check for mappers
			for name, mapper := range mappers {
				expectedMapper, exists := test.expectedMappers[name]
				require.True(t, exists, "mapper %q not expected", name)
				assert.Equal(t, expectedMapper.APIVersion, mapper.APIVersion)
				assert.Equal(t, expectedMapper.Resource, mapper.Resource)
				assert.NotNil(t, mapper.Mapper)
			}
		})
	}
}
