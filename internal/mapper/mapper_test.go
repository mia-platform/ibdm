// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mapper

import (
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMapper(t *testing.T) {
	t.Parallel()

	t.Run("new mapper from valid templates", func(t *testing.T) {
		t.Parallel()
		mapper, err := New("{{ .name }}", map[string]string{
			"key":      "name",
			"otherKey": "{{ .otherKey | trim }}",
		})
		assert.NoError(t, err)
		assert.NotNil(t, mapper)

		mapperInstance, ok := mapper.(*internalMapper)
		require.True(t, ok)
		require.NotNil(t, mapperInstance.idTemplate)
		require.NotNil(t, mapperInstance.specTemplate)
	})

	t.Run("return error when one template is broken", func(t *testing.T) {
		t.Parallel()
		mapper, err := New("{{ .name }}", map[string]string{
			"key":      "name",
			"otherKey": "{{ .otherKey | unknwonFunc }}",
		})
		assert.Nil(t, mapper)
		assert.Error(t, err)
		assert.ErrorContains(t, err, errTemplateParsing)

		var targetError *ParsingError
		assert.ErrorAs(t, err, &targetError)
		joinedErrors, ok := targetError.Unwrap().(interface{ Unwrap() []error })
		require.True(t, ok)
		require.Len(t, joinedErrors.Unwrap(), 1)
	})

	t.Run("return error when one or more template is broken", func(t *testing.T) {
		t.Parallel()
		mapper, err := New("{{ .name | unknwonFunc }}", map[string]string{
			"key":      "name",
			"otherKey": "{{ .otherKey | unknwonFunc }}",
		})
		assert.Nil(t, mapper)
		assert.Error(t, err)
		assert.ErrorContains(t, err, errTemplateParsing)

		var targetError *ParsingError
		assert.ErrorAs(t, err, &targetError)
		joinedErrors, ok := targetError.Unwrap().(interface{ Unwrap() []error })
		require.True(t, ok)
		require.Len(t, joinedErrors.Unwrap(), 2)
	})
}

func TestMapper(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		mapper        Mapper
		input         map[string]any
		expected      MappedData
		expectedError bool
	}{
		"simple mapping": {
			mapper: func() Mapper {
				m, err := New("{{ .name }}", map[string]string{
					"key":           "name",
					"string":        "{{ .name }}",
					"otherKey":      "{{ .otherKey.value }}",
					"nested":        "{{ .otherKey | toJSON }}",
					"array":         "{{ .array | toJSON }}",
					"combinedField": "{{ .name }}-{{ .otherKey.value }}",
				})
				require.NoError(t, err)
				return m
			}(),
			input: map[string]any{
				"name": "example",
				"otherKey": map[string]any{
					"string": "example",
					"value":  42,
				},
				"array": []int{1, 2, 3},
			},
			expected: MappedData{
				Identifier: "example",
				Spec: map[string]any{
					"key":      "name",
					"string":   "example",
					"otherKey": 42,
					"nested": map[string]any{
						"string": "example",
						"value":  42,
					},
					"array":         []any{1, 2, 3},
					"combinedField": "example-42",
				},
			},
		},
		"always casting identifier to a string": {
			mapper: func() Mapper {
				m, err := New("{{ .id }}", map[string]string{
					"key": "name",
				})
				require.NoError(t, err)
				return m
			}(),
			input: map[string]any{
				"id":   12345,
				"name": "example",
			},
			expected: MappedData{
				Identifier: "12345",
				Spec: map[string]any{
					"key": "name",
				},
			},
		},
		"identifier mapping with missing fields": {
			mapper: func() Mapper {
				m, err := New("{{ .name }}-{{ .missingField }}", map[string]string{
					"key": "name",
				})
				require.NoError(t, err)
				return m
			}(),
			input: map[string]any{
				"name": "example",
			},
			expectedError: true,
		},
		"spec mapping with missing fields": {
			mapper: func() Mapper {
				m, err := New("{{ .name }}", map[string]string{
					"key":      "name",
					"otherKey": "{{ .otherKey.value }}",
				})
				require.NoError(t, err)
				return m
			}(),
			input: map[string]any{
				"name": "example",
			},
			expectedError: true,
		},
		"identifier with invalid characters": {
			mapper: func() Mapper {
				m, err := New("{{ .name }}_invalid", map[string]string{
					"key": "name",
				})
				require.NoError(t, err)
				return m
			}(),
			input: map[string]any{
				"name": "example",
			},
			expectedError: true,
		},
		"identifier too long": {
			mapper: func() Mapper {
				m, err := New("{{ .name }}", map[string]string{
					"key": "name",
				})
				require.NoError(t, err)
				return m
			}(),
			input: map[string]any{
				"name": "a-very-long-name-that-exceeds-the-maximum-length-limit-imposed-by-the-identifier-validation-rules-set-in-place-to-ensure-compliance-with-kubernetes-naming-conventions-and-best-practices-which-stipulate-that-identifiers-must-not-only-contain-lowercase-alphanumeric-characters-dashes-or-dots",
			},
			expectedError: true,
		},
		"create string array from object array": {
			mapper: func() Mapper {
				m, err := New("{{ .name }}", map[string]string{
					"key": `{{ pluck "key" .objects | toJSON }}`,
				})
				require.NoError(t, err)
				return m
			}(),
			input: map[string]any{
				"name": "example",
				"objects": []map[string]any{
					{"key": "value1"},
					{"key": "value2"},
					{"key": "value3"},
				},
			},
			expected: MappedData{
				Identifier: "example",
				Spec: map[string]any{
					"key": []interface{}{
						"value1",
						"value2",
						"value3",
					},
				},
			},
		},
		"use get value from missing key": {
			mapper: func() Mapper {
				m, err := New("{{ .name }}", map[string]string{
					"key":       `{{ get "missingKey" . "defaultValue" }}`,
					"nestedKey": `{{ get "nestedKey" .otherKey "defaultValue" }}`,
				})
				require.NoError(t, err)
				return m
			}(),
			input: map[string]any{
				"name": "example",
				"otherKey": map[string]any{
					"nestedKey": "nestedValue",
				},
			},
			expected: MappedData{
				Identifier: "example",
				Spec: map[string]any{
					"key":       "defaultValue",
					"nestedKey": "nestedValue",
				},
			},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			output, err := test.mapper.ApplyTemplates(test.input)
			if test.expectedError {
				var expectedError template.ExecError
				assert.Empty(t, output)
				assert.ErrorAs(t, err, &expectedError)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, test.expected, output)
		})
	}
}
