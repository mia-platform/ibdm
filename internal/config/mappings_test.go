// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package config

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
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
					Type:     "yaml",
					Syncable: true,
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
					Type:     "json",
					Syncable: true,
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
					Type:     "first",
					Syncable: true,
					Mappings: Mappings{
						Identifier: "{{ .spec.id }}",
						Spec: map[string]string{
							"fieldA": "{{ .spec.fieldA }}",
							"fieldB": "{{ .spec.fieldB }}",
						},
					},
				},
				{
					Type:     "second",
					Syncable: true,
					Mappings: Mappings{
						Identifier: "{{ .metadata.name }}",
						Spec: map[string]string{
							"attributeX": "{{ .spec.attributeX }}",
						},
					},
				},
				{
					Type:     "third",
					Syncable: false,
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
		"missing file return error": {
			path:          filepath.Join(tempDir, "missing"),
			expectedError: syscall.ENOENT,
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

func TestInvalidMappingFile(t *testing.T) {
	t.Parallel()

	expectedError := &yaml.TypeError{
		Errors: []string{"line 2: cannot unmarshal !!str `syncable` into bool"},
	}
	mappings, err := NewMappingConfigsFromPath(filepath.Join("testdata", "invalid.yaml"))
	assert.Empty(t, mappings)
	assert.Equal(t, expectedError, err)
}
