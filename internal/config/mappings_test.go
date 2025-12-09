// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package config

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
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
					Type:       "yaml",
					APIVersion: "group/v1",
					Kind:       "configs",
					Syncable:   true,
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
					Type:       "json",
					APIVersion: "group/v1",
					Kind:       "configs",
					Syncable:   true,
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
					Type:       "first",
					APIVersion: "group/v1",
					Kind:       "configs",
					Syncable:   true,
					Mappings: Mappings{
						Identifier: "{{ .spec.id }}",
						Spec: map[string]string{
							"fieldA": "{{ .spec.fieldA }}",
							"fieldB": "{{ .spec.fieldB }}",
						},
					},
				},
				{
					Type:       "second",
					APIVersion: "group/v1",
					Kind:       "configs",
					Syncable:   true,
					Mappings: Mappings{
						Identifier: "{{ .metadata.name }}",
						Spec: map[string]string{
							"attributeX": "{{ .spec.attributeX }}",
						},
					},
				},
				{
					Type:       "third",
					APIVersion: "group/v1",
					Kind:       "configs",
					Syncable:   false,
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
