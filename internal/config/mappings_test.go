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
					ItemFamily: "configs",
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
					ItemFamily: "configs",
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
					ItemFamily: "configs",
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
					ItemFamily: "configs",
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
					ItemFamily: "configs",
					Syncable:   false,
					Extra: map[string]any{
						"apiVersion": "2025-01-04-preview",
					},
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
					Type:       "yaml",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
					Mappings: Mappings{
						Identifier: "{{ .name }}",
						Metadata: MetadataMapping{
							"annotations":       "{{ printf \"%s\" .name }}",
							"creationTimestamp": "{{ printf \"%s\" .name }}",
							"description":       "{{ printf \"%s\" .name }}",
							"labels":            "{{ printf \"%s\" .name }}",
							"links":             "{{ printf \"%s\" .name }}",
							"name":              "{{ printf \"%s\" .name }}",
							"namespace":         "{{ printf \"%s\" .name }}",
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
					Type:       "yaml",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
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
		"valid yaml file with extra mapping": {
			path: filepath.Join("testdata", "extra.yaml"),
			expectedMappingConfigs: []*MappingConfig{
				{
					Type:       "yaml",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
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
								"sourceRef": map[string]any{
									"apiVersion": "{{ .extraValue }}",
									"family":     "{{ .extraValue }}",
									"name":       "{{ .extraValue }}",
								},
								"typeRef": map[string]any{
									"apiVersion": "{{ .extraValue }}",
									"family":     "{{ .extraValue }}",
									"name":       "{{ .extraValue }}",
								},
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
					Type:       "yaml",
					APIVersion: "group/v1",
					ItemFamily: "configs",
					Syncable:   true,
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
								"sourceRef": map[string]any{
									"apiVersion": "{{ .extraValue }}",
									"family":     "{{ .extraValue }}",
									"name":       "{{ .extraValue }}",
								},
								"typeRef": map[string]any{
									"apiVersion": "{{ .extraValue }}",
									"family":     "{{ .extraValue }}",
									"name":       "{{ .extraValue }}",
								},
							},
							{
								"apiVersion":   "group/v1",
								"itemFamily":   "relationships",
								"identifier":   `extra2`,
								"deletePolicy": "none",
								"sourceRef": map[string]any{
									"apiVersion": "{{ .extraValue }}",
									"family":     "{{ .extraValue }}",
									"name":       "{{ .extraValue }}",
								},
								"typeRef": map[string]any{
									"apiVersion": "{{ .extraValue }}",
									"family":     "{{ .extraValue }}",
									"name":       "{{ .extraValue }}",
								},
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
		"valid yaml file with extra mapping of family relationships with missing sourceRef and typeRef": {
			path:          filepath.Join("testdata", "extramissingfields.yaml"),
			expectedError: ErrParsing,
		},
		"valid yaml file with extra mapping of family relationships with empty sourceRef and typeRef": {
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
