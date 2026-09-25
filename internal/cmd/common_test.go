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
	"github.com/mia-platform/ibdm/internal/source/azure"
	azuredevops "github.com/mia-platform/ibdm/internal/source/azure-devops"
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
				azureDevOpsSource + "\t" + azureDevOpsDescription,
				azureSource + "\t" + azureDescription,
				bitbucketSource + "\t" + bitbucketDescription,
				consoleSource + "\t" + consoleDescription,
				gcpSource + "\t" + gcpDescription,
				githubSource + "\t" + githubDescription,
				gitlabSource + "\t" + gitlabDescription,
				nexusSource + "\t" + nexusDescription,
				sonarqubeSource + "\t" + sonarqubeDescription,
				sysdigSource + "\t" + sysdigDescription,
			},
		},
		"some args, no completions": {
			args: []string{gcpSource},
		},
		"no args, partial string, return filtered commands": {
			args:       []string{},
			toComplete: "gc",
			expectedCompletion: []string{
				gcpSource + "\t" + gcpDescription,
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
		"azure integration": {
			integrationName:    azureSource,
			expectedSourceType: (*azure.Source)(nil),
		},
		"azure devops integration": {
			integrationName:    azureDevOpsSource,
			expectedSourceType: (*azuredevops.Source)(nil),
		},
		"gcp integration": {
			integrationName:    gcpSource,
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

func TestCollectPathKubernetesVolumeMount(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupKubernetesVolumeMountTestFileStructure(t, tmpDir)

	files, err := collectPaths([]string{tmpDir})
	require.NoError(t, err)
	expectedFiles := []string{
		filepath.Join(tmpDir, "invalid.yaml"),
		filepath.Join(tmpDir, "file.txt"),
	}
	assert.ElementsMatch(t, expectedFiles, files)
}

func TestLoadMappers(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		paths           []string
		syncOnly        bool
		expectedMappers map[string][]pipeline.DataMapper
		expectedError   error
	}{
		"valid mapping config": {
			paths: []string{
				filepath.Join("testdata", "mappers.yaml"),
			},
			expectedMappers: map[string][]pipeline.DataMapper{
				"valid": {{
					Name:       "valid",
					APIVersion: "v1",
					ItemFamily: "family",
				}},
				"mapper-type": {{
					Name:       "mapper-type",
					APIVersion: "v1",
					ItemFamily: "family",
				}},
			},
		},
		"valid mapping config filtered by sync": {
			paths: []string{
				filepath.Join("testdata", "mappers.yaml"),
			},
			syncOnly: true,
			expectedMappers: map[string][]pipeline.DataMapper{
				"mapper-type": {{
					Name:       "mapper-type",
					APIVersion: "v1",
					ItemFamily: "family",
				}},
			},
		},
		"mappings sharing a type are all kept in load order": {
			paths: []string{
				filepath.Join("testdata", "mappers.yaml"),
				filepath.Join("testdata", "same-type.yaml"),
			},
			expectedMappers: map[string][]pipeline.DataMapper{
				"valid": {
					{
						Name:       "valid",
						APIVersion: "v1",
						ItemFamily: "family",
					},
					{
						Name:       "same-type",
						APIVersion: "v2",
						ItemFamily: "other-family",
						Extra:      map[string]any{"apiVersion": "2024-01-01"},
					},
				},
				"mapper-type": {{
					Name:       "mapper-type",
					APIVersion: "v1",
					ItemFamily: "family",
				}},
			},
		},
		"error reading config": {
			paths: []string{
				filepath.Join("testdata", "invalid-config.txt"),
			},
			expectedError: config.ErrParsing,
		},
		"duplicate mapping names across files": {
			paths: []string{
				filepath.Join("testdata", "mappers.yaml"),
				filepath.Join("testdata", "duplicate-name.yaml"),
			},
			expectedError: config.ErrDuplicateMappingName,
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
			require.Len(t, mappers, len(test.expectedMappers))
			for name, typeMappers := range mappers {
				expectedTypeMappers, exists := test.expectedMappers[name]
				require.True(t, exists, "mapper %q not expected", name)
				require.Len(t, typeMappers, len(expectedTypeMappers), "mappers for type %q", name)
				for i, mapper := range typeMappers {
					assert.Equal(t, expectedTypeMappers[i].Name, mapper.Name)
					assert.Equal(t, expectedTypeMappers[i].APIVersion, mapper.APIVersion)
					assert.Equal(t, expectedTypeMappers[i].ItemFamily, mapper.ItemFamily)
					assert.Equal(t, expectedTypeMappers[i].Extra, mapper.Extra)
					assert.NotNil(t, mapper.Mapper)
				}
			}
		})
	}
}

// TestLoadMappersLexicalOrder asserts that mappings sharing a type reach the
// pipeline in lexical file order, whatever order the files were created in.
func TestLoadMappersLexicalOrder(t *testing.T) {
	t.Parallel()

	const mappingTemplate = `type: my-type
apiVersion: v1
itemFamily: family
mappings:
  identifier: "{{ .id }}"
  spec:
    field1: "{{ .field1 }}"
`

	tmpDir := t.TempDir()
	for _, fileName := range []string{"b-mapping.yaml", "c-mapping.yaml", "a-mapping.yaml"} {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, fileName), []byte(mappingTemplate), 0o600))
	}

	for range 3 {
		paths, err := collectPaths([]string{tmpDir})
		require.NoError(t, err)

		mappers, err := loadMappers(paths, false)
		require.NoError(t, err)
		require.Len(t, mappers, 1)

		names := make([]string, 0, len(mappers["my-type"]))
		for _, mapper := range mappers["my-type"] {
			names = append(names, mapper.Name)
		}
		require.Equal(t, []string{"a-mapping", "b-mapping", "c-mapping"}, names)
	}
}
