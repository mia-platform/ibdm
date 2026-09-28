// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMappingsFromPath(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	testCases := map[string]struct {
		path                   string
		expectedMappingConfigs []*MappingConfig
		expectedError          error
	}{
		"valid yaml file with one mapping": {
			path: filepath.Join("testdata", "one.yaml"),
			expectedMappingConfigs: []*MappingConfig{
				{
					Name:       "one",
					Type:       "yaml",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
					path:       filepath.Join("testdata", "one.yaml"),
					Mappings: Mappings{
						Identifier: "{{ .name }}",
						Spec: map[string]string{
							"key":      "{{ .value }}",
							"otherKey": "{{ .otherValue | functionName }}",
						},
					},
				},
			},
		},
		"valid json file with one mapping": {
			path: filepath.Join("testdata", "one.json"),
			expectedMappingConfigs: []*MappingConfig{
				{
					Name:       "one",
					Type:       "json",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
					path:       filepath.Join("testdata", "one.json"),
					Mappings: Mappings{
						Identifier: "{{ .name }}",
						Spec: map[string]string{
							"key":      "{{ .value }}",
							"otherKey": "{{ .otherValue | functionName }}",
						},
					},
				},
			},
		},
		"valid yaml file with multiple mappings": {
			path: filepath.Join("testdata", "multiple.yaml"),
			expectedMappingConfigs: []*MappingConfig{
				{
					Name:       "first-mapping",
					Type:       "first",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
					path:       filepath.Join("testdata", "multiple.yaml"),
					Mappings: Mappings{
						Identifier: "{{ .spec.id }}",
						Spec: map[string]string{
							"fieldA": "{{ .spec.fieldA }}",
							"fieldB": "{{ .spec.fieldB }}",
						},
					},
				},
				{
					Name:       "second-mapping",
					Type:       "second",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
					path:       filepath.Join("testdata", "multiple.yaml"),
					Mappings: Mappings{
						Identifier: "{{ .metadata.name }}",
						Spec: map[string]string{
							"attributeX": "{{ .spec.attributeX }}",
						},
					},
				},
				{
					Name:       "third-mapping",
					Type:       "third",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   false,
					Extra: map[string]any{
						"apiVersion": "2025-01-04-preview",
					},
					path: filepath.Join("testdata", "multiple.yaml"),
					Mappings: Mappings{
						Identifier: "{{ .spec.code }}",
						Spec: map[string]string{
							"detail1": "{{ .spec.detail1 }}",
							"detail2": "{{ .spec.detail2 }}",
						},
					},
				},
			},
		},
		"missing data return error": {
			path:          filepath.Join("testdata", "missingdata.yaml"),
			expectedError: ErrParsing,
		},
		"missing file return error": {
			path:          filepath.Join(tempDir, "missing"),
			expectedError: syscall.ENOENT,
		},
		"invalid mapping file return error": {
			path:          filepath.Join("testdata", "invalid.yaml"),
			expectedError: ErrParsing,
		},
		"valid yaml file with metadata mapping": {
			path: filepath.Join("testdata", "metadatamapping.yaml"),
			expectedMappingConfigs: []*MappingConfig{
				{
					Name:       "metadatamapping",
					Type:       "yaml",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
					path:       filepath.Join("testdata", "metadatamapping.yaml"),
					Mappings: Mappings{
						Identifier: "{{ .name }}",
						Metadata: MetadataMapping{
							"annotations":       "{{ printf \"%s\" .name }}",
							"creationTimestamp": "{{ printf \"%s\" .name }}",
							"description":       "{{ printf \"%s\" .name }}",
							"labels":            "{{ printf \"%s\" .name }}",
							"links":             "{{ printf \"%s\" .name }}",
							"name":              "{{ printf \"%s\" .name }}",
							"tags":              "{{ printf \"%s\" .name }}",
							"title":             "{{ printf \"%s\" .name }}",
							"uid":               "{{ printf \"%s\" .name }}",
						},
						Spec: map[string]string{
							"key":      "{{ .value }}",
							"otherKey": "{{ .otherValue | functionName }}",
						},
					},
				},
			},
		},
		"wrong metadata file prune unknown fields": {
			path: filepath.Join("testdata", "wrongmetadata.yaml"),
			expectedMappingConfigs: []*MappingConfig{
				{
					Name:       "wrongmetadata",
					Type:       "yaml",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
					path:       filepath.Join("testdata", "wrongmetadata.yaml"),
					Mappings: Mappings{
						Identifier: "{{ .name }}",
						Metadata:   MetadataMapping{},
						Spec: map[string]string{
							"key":      "{{ .value }}",
							"otherKey": "{{ .otherValue | functionName }}",
						},
					},
				},
			},
		},
		"valid yaml file with createIf guard": {
			path: filepath.Join("testdata", "createif.yaml"),
			expectedMappingConfigs: []*MappingConfig{
				{
					Name:       "createif",
					Type:       "yaml",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
					CreateIf:   `{{ eq .kind "functionapp" }}`,
					path:       filepath.Join("testdata", "createif.yaml"),
					Mappings: Mappings{
						Identifier: "{{ .name }}",
						Spec: map[string]string{
							"key": "{{ .value }}",
						},
					},
				},
			},
		},
		"valid yaml file with extra mapping": {
			path: filepath.Join("testdata", "extra.yaml"),
			expectedMappingConfigs: []*MappingConfig{
				{
					Name:       "extra",
					Type:       "yaml",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
					path:       filepath.Join("testdata", "extra.yaml"),
					Mappings: Mappings{
						Identifier: "{{ .name }}",
						Spec: map[string]string{
							"key":      "{{ .value }}",
							"otherKey": "{{ .otherValue | functionName }}",
						},
						Extra: []Extra{
							{
								"apiVersion":   "group/v1",
								"itemFamily":   "relationships",
								"identifier":   `extra1`,
								"deletePolicy": "none",
								"sourceRef":    "{{ .extraValue }}",
								"targetRef":    "{{ .extraValue }}",
								"typeRef":      "{{ .extraValue }}",
							},
						},
					},
				},
			},
		},
		"valid yaml file with two extra mapping": {
			path: filepath.Join("testdata", "twoextra.yaml"),
			expectedMappingConfigs: []*MappingConfig{
				{
					Name:       "twoextra",
					Type:       "yaml",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
					path:       filepath.Join("testdata", "twoextra.yaml"),
					Mappings: Mappings{
						Identifier: "{{ .name }}",
						Spec: map[string]string{
							"key":      "{{ .value }}",
							"otherKey": "{{ .otherValue | functionName }}",
						},
						Extra: []Extra{
							{
								"apiVersion":   "group/v1",
								"itemFamily":   "relationships",
								"identifier":   `extra1`,
								"deletePolicy": "none",
								"sourceRef":    "{{ .extraValue }}",
								"targetRef":    "{{ .extraValue }}",
								"typeRef":      "{{ .extraValue }}",
							},
							{
								"apiVersion":   "group/v1",
								"itemFamily":   "relationships",
								"identifier":   `extra2`,
								"deletePolicy": "none",
								"sourceRef":    "{{ .extraValue }}",
								"targetRef":    "{{ .extraValue }}",
								"typeRef":      "{{ .extraValue }}",
							},
						},
					},
				},
			},
		},
		"valid yaml file with extra mapping with invalid itemFamily": {
			path:          filepath.Join("testdata", "extrainvalidfamily.yaml"),
			expectedError: ErrParsing,
		},
		"valid yaml file with extra mapping of family relationships with missing sourceRef, targetRef and typeRef": {
			path:          filepath.Join("testdata", "extramissingfields.yaml"),
			expectedError: ErrParsing,
		},
		"valid yaml file with extra mapping of family relationships with invalid sourceRef, targetRef and typeRef": {
			path:          filepath.Join("testdata", "extrainvalidfields.yaml"),
			expectedError: ErrParsing,
		},
		"valid yaml file with extra mapping of family relationships with empty sourceRef, targetRef and typeRef": {
			path:          filepath.Join("testdata", "extraemptyfields.yaml"),
			expectedError: ErrParsing,
		},
		"wrong extra file return error": {
			path:          filepath.Join("testdata", "wrongextra.yaml"),
			expectedError: ErrParsing,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			mappingConfigs, err := NewMappingConfigsFromPath(test.path)
			if test.expectedError != nil {
				assert.Empty(t, mappingConfigs)
				assert.ErrorIs(t, err, test.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, test.expectedMappingConfigs, mappingConfigs)
		})
	}
}

// mappingDocument returns a minimal valid mapping document, declaring name when not empty.
func mappingDocument(name string) string {
	document := `type: my-type
apiVersion: group/v1
itemFamily: configs
mappings:
  identifier: "{{ .name }}"
  spec:
    key: "{{ .value }}"
`
	if name != "" {
		document = "name: " + name + "\n" + document
	}
	return document
}

func TestNewMappingConfigsFromPathNames(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		fileName      string
		content       string
		expectedNames []string
		expectedError error
	}{
		"name defaults to the file base name": {
			fileName:      "my-repositories.yaml",
			content:       mappingDocument(""),
			expectedNames: []string{"my-repositories"},
		},
		"name defaults to the file base name keeping inner dots": {
			fileName:      "my-repositories.v2.yaml",
			content:       mappingDocument(""),
			expectedNames: []string{"my-repositories.v2"},
		},
		"name defaults to the file base name without extension": {
			fileName:      "apim_services",
			content:       mappingDocument(""),
			expectedNames: []string{"apim_services"},
		},
		"explicit name wins over the file base name": {
			fileName:      "my-repositories.yaml",
			content:       mappingDocument("my-custom-name"),
			expectedNames: []string{"my-custom-name"},
		},
		"explicit name allows a non compliant file name": {
			fileName:      "My Repositories.yaml",
			content:       mappingDocument("my-repositories"),
			expectedNames: []string{"my-repositories"},
		},
		"non compliant file base name without explicit name return error": {
			fileName:      "My Repositories.yaml",
			content:       mappingDocument(""),
			expectedError: ErrParsing,
		},
		"multiple mappings with names": {
			fileName:      "my-mappings.yaml",
			content:       mappingDocument("first") + "---\n" + mappingDocument("second"),
			expectedNames: []string{"first", "second"},
		},
		"multiple mappings with a missing name return error": {
			fileName:      "my-mappings.yaml",
			content:       mappingDocument("first") + "---\n" + mappingDocument(""),
			expectedError: ErrParsing,
		},
		"multiple mappings without names return error": {
			fileName:      "my-mappings.yaml",
			content:       mappingDocument("") + "---\n" + mappingDocument(""),
			expectedError: ErrParsing,
		},
		"name with uppercase characters return error": {
			fileName:      "my-mappings.yaml",
			content:       mappingDocument("My-Mapping"),
			expectedError: ErrParsing,
		},
		"name with invalid characters return error": {
			fileName:      "my-mappings.yaml",
			content:       mappingDocument("my/mapping"),
			expectedError: ErrParsing,
		},
		"name ending with a separator return error": {
			fileName:      "my-mappings.yaml",
			content:       mappingDocument("my-mapping-"),
			expectedError: ErrParsing,
		},
		"name starting with a separator return error": {
			fileName:      "my-mappings.yaml",
			content:       mappingDocument("_my-mapping"),
			expectedError: ErrParsing,
		},
		"name at the maximum length": {
			fileName:      "my-mappings.yaml",
			content:       mappingDocument(strings.Repeat("a", maxMappingNameLength)),
			expectedNames: []string{strings.Repeat("a", maxMappingNameLength)},
		},
		"name over the maximum length return error": {
			fileName:      "my-mappings.yaml",
			content:       mappingDocument(strings.Repeat("a", maxMappingNameLength+1)),
			expectedError: ErrParsing,
		},
		"single character name": {
			fileName:      "a.yaml",
			content:       mappingDocument(""),
			expectedNames: []string{"a"},
		},
		"empty file return no mappings": {
			fileName:      "empty.yaml",
			content:       "",
			expectedNames: []string{},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), test.fileName)
			require.NoError(t, os.WriteFile(path, []byte(test.content), 0o600))

			mappingConfigs, err := NewMappingConfigsFromPath(path)
			if test.expectedError != nil {
				require.ErrorIs(t, err, test.expectedError)
				require.ErrorContains(t, err, path)
				require.Empty(t, mappingConfigs)
				return
			}

			require.NoError(t, err)
			names := make([]string, 0, len(mappingConfigs))
			for _, mappingConfig := range mappingConfigs {
				names = append(names, mappingConfig.Name)
				require.Equal(t, path, mappingConfig.path)
			}
			require.Equal(t, test.expectedNames, names)
		})
	}
}

func TestValidateMappingNames(t *testing.T) {
	t.Parallel()

	firstPath := filepath.Join("my-dir", "first.yaml")
	secondPath := filepath.Join("my-other-dir", "second.yaml")

	testCases := map[string]struct {
		mappings      []*MappingConfig
		expectedError error
		expectedPaths []string
	}{
		"no mappings": {
			mappings: []*MappingConfig{},
		},
		"unique names": {
			mappings: []*MappingConfig{
				{Name: "first", Type: "my-type", path: firstPath},
				{Name: "second", Type: "my-type", path: secondPath},
			},
		},
		"duplicate names across two files return error": {
			mappings: []*MappingConfig{
				{Name: "my-mapping", Type: "my-type", path: firstPath},
				{Name: "my-mapping", Type: "my-other-type", path: secondPath},
			},
			expectedError: ErrDuplicateMappingName,
			expectedPaths: []string{firstPath, secondPath},
		},
		"duplicate names in the same file return error": {
			mappings: []*MappingConfig{
				{Name: "first", Type: "my-type", path: firstPath},
				{Name: "my-mapping", Type: "my-type", path: secondPath},
				{Name: "my-mapping", Type: "my-other-type", path: secondPath},
			},
			expectedError: ErrDuplicateMappingName,
			expectedPaths: []string{secondPath},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := ValidateMappingNames(test.mappings)
			if test.expectedError != nil {
				require.ErrorIs(t, err, test.expectedError)
				for _, path := range test.expectedPaths {
					require.ErrorContains(t, err, path)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestNewMappingConfigsFromReaderMatchesPath(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{"one.yaml", "one.json", "multiple.yaml", "extra.yaml", "createif.yaml", "missingdata.yaml", "invalid.yaml"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("testdata", fixture)
			fromPath, pathErr := NewMappingConfigsFromPath(path)

			file, err := os.Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = file.Close() })
			fromReader, readerErr := NewMappingConfigsFromReader(file, path)

			require.Equal(t, fromPath, fromReader)
			if pathErr != nil {
				require.EqualError(t, readerErr, pathErr.Error())
			} else {
				require.NoError(t, readerErr)
			}
		})
	}
}

func TestNewMappingConfigsFromFS(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"console/my-projects.yaml": {Data: []byte(mappingDocument(""))},
		"console/broken.yaml":      {Data: []byte("type: [")},
	}

	t.Run("name defaults to the base name of the fs path", func(t *testing.T) {
		t.Parallel()

		configs, err := NewMappingConfigsFromFS(fsys, "console/my-projects.yaml")
		require.NoError(t, err)
		require.Len(t, configs, 1)
		require.Equal(t, "my-projects", configs[0].Name)
		require.Equal(t, "console/my-projects.yaml", configs[0].path)
	})

	t.Run("parse errors name the fs path", func(t *testing.T) {
		t.Parallel()

		_, err := NewMappingConfigsFromFS(fsys, "console/broken.yaml")
		require.ErrorIs(t, err, ErrParsing)
		require.ErrorContains(t, err, "console/broken.yaml")
	})

	t.Run("missing file returns the fs error", func(t *testing.T) {
		t.Parallel()

		_, err := NewMappingConfigsFromFS(fsys, "console/missing.yaml")
		require.ErrorIs(t, err, fs.ErrNotExist)
	})
}
